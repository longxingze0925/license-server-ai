using System.Net;
using System.Net.Http.Headers;
using System.Net.Http.Json;
using System.Runtime.InteropServices;
using System.Security.Cryptography;
using System.Text;
using System.Text.Json;
using System.Text.Json.Serialization;

namespace LicenseServer.Sdk;

/// <summary>
/// Client-side SDK for /api/client/* endpoints.
/// </summary>
public sealed class LicenseClient : IDisposable
{
    private static readonly TimeSpan TokenRefreshSkew = TimeSpan.FromSeconds(60);

    private readonly HttpClient _http;
    private readonly bool _disposeHttpClient;
    private bool _disposed;
    private string _baseUrl;

    public LicenseClient(
        string baseUrl,
        string appKey,
        string? machineId = null,
        IClientSessionStore? sessions = null,
        HttpClient? httpClient = null,
        bool disposeHttpClient = false)
    {
        if (string.IsNullOrWhiteSpace(baseUrl))
        {
            throw new ArgumentException("Base URL cannot be empty.", nameof(baseUrl));
        }
        if (string.IsNullOrWhiteSpace(appKey))
        {
            throw new ArgumentException("App key cannot be empty.", nameof(appKey));
        }

        _baseUrl = NormalizeBaseUrl(baseUrl);
        AppKey = appKey.Trim();
        MachineId = string.IsNullOrWhiteSpace(machineId) ? CreateDefaultMachineId() : machineId.Trim();
        Sessions = sessions ?? new InMemoryClientSessionStore();
        _http = httpClient ?? new HttpClient();
        _disposeHttpClient = httpClient is null || disposeHttpClient;
        AddUserAgent(_http);

        HotUpdates = new HotUpdateApi(this);
        DataSync = new DataSyncApi(this);
        Scripts = new ScriptApi(this);
        SecureScripts = new SecureScriptApi(this);
    }

    public string BaseUrl => _baseUrl;

    public string AppKey { get; }

    public string MachineId { get; }

    public IClientSessionStore Sessions { get; }

    public HotUpdateApi HotUpdates { get; }

    public DataSyncApi DataSync { get; }

    public ScriptApi Scripts { get; }

    public SecureScriptApi SecureScripts { get; }

    internal HttpClient Http => _http;

    public void SetBaseUrl(string baseUrl)
    {
        if (string.IsNullOrWhiteSpace(baseUrl))
        {
            throw new ArgumentException("Base URL cannot be empty.", nameof(baseUrl));
        }

        _baseUrl = NormalizeBaseUrl(baseUrl);
    }

    public LicenseWebSocketClient CreateWebSocketClient()
        => new(this);

    public async Task<ClientLicenseInfo> ActivateAsync(string licenseKey, ClientDeviceInfo? deviceInfo = null, CancellationToken ct = default)
    {
        if (string.IsNullOrWhiteSpace(licenseKey))
        {
            throw new ArgumentException("License key cannot be empty.", nameof(licenseKey));
        }

        var result = await PostAsync<ClientLicenseInfo>("/auth/activate", new
        {
            app_key = AppKey,
            license_key = licenseKey,
            machine_id = MachineId,
            device_info = deviceInfo ?? CreateDeviceInfo(),
        }, ct: ct).ConfigureAwait(false);

        SaveSession(result, email: null);
        return result;
    }

    public async Task<ClientLicenseInfo> LoginAsync(string email, string password, ClientDeviceInfo? deviceInfo = null, CancellationToken ct = default)
    {
        if (string.IsNullOrWhiteSpace(email))
        {
            throw new ArgumentException("Email cannot be empty.", nameof(email));
        }
        if (string.IsNullOrWhiteSpace(password))
        {
            throw new ArgumentException("Password cannot be empty.", nameof(password));
        }

        var normalizedEmail = email.Trim();
        var result = await PostAsync<ClientLicenseInfo>("/auth/login", new
        {
            app_key = AppKey,
            email = normalizedEmail,
            password = HashPassword(password, normalizedEmail),
            password_hashed = true,
            machine_id = MachineId,
            device_info = deviceInfo ?? CreateDeviceInfo(),
        }, ct: ct).ConfigureAwait(false);

        result.Email = normalizedEmail;
        SaveSession(result, normalizedEmail);
        return result;
    }

    public Task<ClientRegisterResult> RegisterAsync(string email, string password, string? name = null, CancellationToken ct = default)
    {
        if (string.IsNullOrWhiteSpace(email))
        {
            throw new ArgumentException("Email cannot be empty.", nameof(email));
        }
        if (string.IsNullOrWhiteSpace(password))
        {
            throw new ArgumentException("Password cannot be empty.", nameof(password));
        }

        var normalizedEmail = email.Trim();
        return PostAsync<ClientRegisterResult>("/auth/register", new
        {
            app_key = AppKey,
            email = normalizedEmail,
            password,
            password_hashed = false,
            name,
        }, ct: ct);
    }

    public Task<ClientLicenseInfo> VerifyLicenseAsync(CancellationToken ct = default)
        => PostAsync<ClientLicenseInfo>("/auth/verify", new
        {
            app_key = AppKey,
            machine_id = MachineId,
        }, ct: ct);

    public Task<ClientLicenseInfo> VerifySubscriptionAsync(CancellationToken ct = default)
        => PostAsync<ClientLicenseInfo>("/subscription/verify", new
        {
            app_key = AppKey,
            machine_id = MachineId,
        }, ct: ct);

    public Task<ClientLicenseInfo> VerifyAsync(CancellationToken ct = default)
    {
        var session = Sessions.GetSession();
        return string.Equals(session?.AuthMode, "subscription", StringComparison.OrdinalIgnoreCase)
            ? VerifySubscriptionAsync(ct)
            : VerifyLicenseAsync(ct);
    }

    public Task<ClientHeartbeatResult> HeartbeatAsync(string? appVersion = null, CancellationToken ct = default)
        => PostAsync<ClientHeartbeatResult>("/auth/heartbeat", new
        {
            app_key = AppKey,
            machine_id = MachineId,
            app_version = appVersion,
        }, ct: ct);

    public Task<ClientHeartbeatResult> SubscriptionHeartbeatAsync(string? appVersion = null, CancellationToken ct = default)
        => PostAsync<ClientHeartbeatResult>("/subscription/heartbeat", new
        {
            app_key = AppKey,
            machine_id = MachineId,
            app_version = appVersion,
        }, ct: ct);

    public Task<ClientHeartbeatResult> SendHeartbeatAsync(string? appVersion = null, CancellationToken ct = default)
    {
        var session = Sessions.GetSession();
        return string.Equals(session?.AuthMode, "subscription", StringComparison.OrdinalIgnoreCase)
            ? SubscriptionHeartbeatAsync(appVersion, ct)
            : HeartbeatAsync(appVersion, ct);
    }

    public async Task<ClientSession> RefreshSessionAsync(CancellationToken ct = default)
    {
        var session = Sessions.GetSession()
            ?? throw new InvalidOperationException("No client session is available.");
        if (session.IsRefreshExpired(TokenRefreshSkew))
        {
            Sessions.Clear();
            throw new LicenseServerException("Client refresh token is expired.", new InvalidOperationException("Refresh token expired."));
        }

        var tokens = await PostAsync<ClientSessionTokenResponse>("/auth/refresh", new
        {
            refresh_token = session.RefreshToken,
        }, authorize: false, allowRefresh: false, ct: ct).ConfigureAwait(false);

        ApplyTokenResponse(session, tokens);
        Sessions.Save(session);
        return session;
    }

    public async Task LogoutAsync(CancellationToken ct = default)
    {
        try
        {
            await SendNoDataAsync(HttpMethod.Post, "/auth/logout", body: null, query: null, authorize: true, allowRefresh: true, ct).ConfigureAwait(false);
        }
        finally
        {
            Sessions.Clear();
        }
    }

    public async Task UnbindCurrentDeviceAsync(string? password = null, CancellationToken ct = default)
    {
        var session = Sessions.GetSession();
        object? body = null;
        if (!string.IsNullOrWhiteSpace(password))
        {
            var email = session?.Email ?? string.Empty;
            body = string.IsNullOrWhiteSpace(email)
                ? new { password }
                : new { password = HashPassword(password, email), password_hashed = true };
        }

        await SendNoDataAsync(HttpMethod.Delete, "/devices/self", body, query: null, authorize: true, allowRefresh: true, ct).ConfigureAwait(false);
        Sessions.Clear();
    }

    public async Task ChangePasswordAsync(string oldPassword, string newPassword, CancellationToken ct = default)
    {
        if (string.IsNullOrWhiteSpace(oldPassword))
        {
            throw new ArgumentException("Old password cannot be empty.", nameof(oldPassword));
        }
        if (string.IsNullOrWhiteSpace(newPassword))
        {
            throw new ArgumentException("New password cannot be empty.", nameof(newPassword));
        }

        var session = Sessions.GetSession()
            ?? throw new InvalidOperationException("No client session is available.");
        if (string.IsNullOrWhiteSpace(session.Email))
        {
            throw new InvalidOperationException("Current session does not include an email address.");
        }

        await SendNoDataAsync(HttpMethod.Put, "/auth/password", new
        {
            old_password = oldPassword,
            new_password = newPassword,
            password_hashed = false,
        }, query: null, authorize: true, allowRefresh: true, ct).ConfigureAwait(false);
    }

    public Task<T> GetAsync<T>(string path, IReadOnlyDictionary<string, string?>? query = null, CancellationToken ct = default)
        => SendJsonAsync<T>(HttpMethod.Get, path, body: null, query, authorize: true, allowRefresh: true, ct);

    public Task<T> PostAsync<T>(string path, object? body, IReadOnlyDictionary<string, string?>? query = null, CancellationToken ct = default)
        => SendJsonAsync<T>(HttpMethod.Post, path, body, query, authorize: true, allowRefresh: true, ct);

    public Task<T> PutAsync<T>(string path, object? body, IReadOnlyDictionary<string, string?>? query = null, CancellationToken ct = default)
        => SendJsonAsync<T>(HttpMethod.Put, path, body, query, authorize: true, allowRefresh: true, ct);

    public Task<T> DeleteAsync<T>(string path, object? body = null, IReadOnlyDictionary<string, string?>? query = null, CancellationToken ct = default)
        => SendJsonAsync<T>(HttpMethod.Delete, path, body, query, authorize: true, allowRefresh: true, ct);

    internal Task<T> PostAsync<T>(string path, object? body, bool authorize, bool allowRefresh, CancellationToken ct)
        => SendJsonAsync<T>(HttpMethod.Post, path, body, query: null, authorize, allowRefresh, ct);

    internal async Task<HttpResponseMessage> SendRawAsync(HttpMethod method, string urlOrPath, HttpContent? content, IReadOnlyDictionary<string, string?>? query = null, bool authorize = true, bool allowRefresh = true, CancellationToken ct = default)
    {
        var bufferedContent = content is null ? null : await BufferedHttpContent.CreateAsync(content, ct).ConfigureAwait(false);
        HttpResponseMessage resp;
        using (var req = BuildRequest(method, urlOrPath, bufferedContent?.ToHttpContent(), query, authorize))
        {
            resp = await _http.SendAsync(req, HttpCompletionOption.ResponseHeadersRead, ct).ConfigureAwait(false);
        }

        if (resp.StatusCode != HttpStatusCode.Unauthorized || !allowRefresh)
        {
            return resp;
        }

        var body = await LicenseServerClient.SafeReadAsync(resp, ct).ConfigureAwait(false);
        resp.Dispose();

        if (!await TryRefreshSessionAsync(ct).ConfigureAwait(false))
        {
            Sessions.Clear();
            throw new LicenseServerException(HttpStatusCode.Unauthorized, LicenseServerClient.TryParseBusinessCode(body), LicenseServerClient.TryParseMessage(body) ?? "Unauthorized.", body);
        }

        using (var req = BuildRequest(method, urlOrPath, bufferedContent?.ToHttpContent(), query, authorize))
        {
            return await _http.SendAsync(req, HttpCompletionOption.ResponseHeadersRead, ct).ConfigureAwait(false);
        }
    }

    internal IReadOnlyDictionary<string, string?> ClientQuery(IReadOnlyDictionary<string, string?>? extra = null)
    {
        var query = new Dictionary<string, string?>(StringComparer.Ordinal)
        {
            ["app_key"] = AppKey,
            ["machine_id"] = MachineId,
        };

        if (extra is not null)
        {
            foreach (var item in extra)
            {
                query[item.Key] = item.Value;
            }
        }

        return query;
    }

    internal string BuildAbsoluteUrl(string urlOrPath)
    {
        if (Uri.TryCreate(urlOrPath, UriKind.Absolute, out _))
        {
            return urlOrPath;
        }

        if (!urlOrPath.StartsWith('/'))
        {
            urlOrPath = "/" + urlOrPath;
        }

        return BaseUrl + urlOrPath;
    }

    internal Uri BuildWebSocketUri()
    {
        var builder = new UriBuilder(BaseUrl)
        {
            Scheme = BaseUrl.StartsWith("https://", StringComparison.OrdinalIgnoreCase) ? "wss" : "ws",
            Path = "/api/client/ws",
            Query = string.Empty,
        };
        return builder.Uri;
    }

    internal static string HashPassword(string password, string email)
    {
        var salted = $"{password}:{email.Trim().ToLowerInvariant()}:license_salt_v1";
        var bytes = SHA256.HashData(Encoding.UTF8.GetBytes(salted));
        return Convert.ToHexString(bytes).ToLowerInvariant();
    }

    internal static ClientDeviceInfo CreateDeviceInfo(string? appVersion = null)
        => new()
        {
            Name = Environment.MachineName,
            Hostname = Environment.MachineName,
            OS = RuntimeInformation.OSDescription,
            OSVersion = Environment.OSVersion.VersionString,
            AppVersion = appVersion ?? string.Empty,
        };

    internal static string BuildQuery(IReadOnlyDictionary<string, string?>? query)
    {
        if (query is null || query.Count == 0)
        {
            return string.Empty;
        }

        var parts = new List<string>(query.Count);
        foreach (var item in query)
        {
            if (string.IsNullOrWhiteSpace(item.Key) || item.Value is null)
            {
                continue;
            }

            parts.Add(Uri.EscapeDataString(item.Key) + "=" + Uri.EscapeDataString(item.Value));
        }

        return parts.Count == 0 ? string.Empty : "?" + string.Join("&", parts);
    }

    internal static void EnsureNotEmpty(string value, string parameterName)
    {
        if (string.IsNullOrWhiteSpace(value))
        {
            throw new ArgumentException(parameterName + " cannot be empty.", parameterName);
        }
    }

    private async Task<T> SendJsonAsync<T>(
        HttpMethod method,
        string path,
        object? body,
        IReadOnlyDictionary<string, string?>? query,
        bool authorize,
        bool allowRefresh,
        CancellationToken ct)
    {
        HttpResponseMessage? resp = null;
        try
        {
            resp = await SendJsonAttemptAsync(method, path, body, query, authorize, allowRefresh, ct).ConfigureAwait(false);
            var raw = await LicenseServerClient.SafeReadAsync(resp, ct).ConfigureAwait(false);

            if (!resp.IsSuccessStatusCode)
            {
                throw new LicenseServerException(resp.StatusCode, LicenseServerClient.TryParseBusinessCode(raw), LicenseServerClient.TryParseMessage(raw) ?? $"HTTP {(int)resp.StatusCode}", raw);
            }

            var parsed = JsonSerializer.Deserialize<ApiResponse<T>>(raw, LicenseServerClient.JsonOptions)
                ?? throw new LicenseServerException(resp.StatusCode, 0, "Response cannot be parsed.", raw);
            if (parsed.Code != 0)
            {
                throw new LicenseServerException(resp.StatusCode, parsed.Code, parsed.Message ?? "Unknown server error.", raw);
            }

            if (parsed.Data is null && default(T) is not null)
            {
                throw new LicenseServerException(resp.StatusCode, 0, "Response data is empty.", raw);
            }

            return parsed.Data!;
        }
        catch (LicenseServerException)
        {
            throw;
        }
        catch (HttpRequestException ex)
        {
            throw new LicenseServerException("Network error: " + ex.Message, ex);
        }
        catch (TaskCanceledException ex) when (!ct.IsCancellationRequested)
        {
            throw new LicenseServerException("Request timed out.", ex);
        }
        finally
        {
            resp?.Dispose();
        }
    }

    internal async Task SendNoDataAsync(
        HttpMethod method,
        string path,
        object? body,
        IReadOnlyDictionary<string, string?>? query = null,
        bool authorize = true,
        bool allowRefresh = true,
        CancellationToken ct = default)
    {
        HttpResponseMessage? resp = null;
        try
        {
            resp = await SendJsonAttemptAsync(method, path, body, query, authorize, allowRefresh, ct).ConfigureAwait(false);
            var raw = await LicenseServerClient.SafeReadAsync(resp, ct).ConfigureAwait(false);
            if (!resp.IsSuccessStatusCode)
            {
                throw new LicenseServerException(resp.StatusCode, LicenseServerClient.TryParseBusinessCode(raw), LicenseServerClient.TryParseMessage(raw) ?? $"HTTP {(int)resp.StatusCode}", raw);
            }

            var parsed = JsonSerializer.Deserialize<ApiResponse<JsonElement?>>(raw, LicenseServerClient.JsonOptions)
                ?? throw new LicenseServerException(resp.StatusCode, 0, "Response cannot be parsed.", raw);
            if (parsed.Code != 0)
            {
                throw new LicenseServerException(resp.StatusCode, parsed.Code, parsed.Message ?? "Unknown server error.", raw);
            }
        }
        finally
        {
            resp?.Dispose();
        }
    }

    private async Task<HttpResponseMessage> SendJsonAttemptAsync(
        HttpMethod method,
        string path,
        object? body,
        IReadOnlyDictionary<string, string?>? query,
        bool authorize,
        bool allowRefresh,
        CancellationToken ct)
    {
        HttpContent? content = body is null ? null : JsonContent.Create(body, options: LicenseServerClient.JsonOptions);
        HttpResponseMessage resp;
        using (var req = BuildRequest(method, path, content, query, authorize))
        {
            resp = await _http.SendAsync(req, HttpCompletionOption.ResponseHeadersRead, ct).ConfigureAwait(false);
        }

        if (resp.StatusCode != HttpStatusCode.Unauthorized || !allowRefresh)
        {
            return resp;
        }

        var raw = await LicenseServerClient.SafeReadAsync(resp, ct).ConfigureAwait(false);
        resp.Dispose();

        if (!await TryRefreshSessionAsync(ct).ConfigureAwait(false))
        {
            Sessions.Clear();
            throw new LicenseServerException(HttpStatusCode.Unauthorized, LicenseServerClient.TryParseBusinessCode(raw), LicenseServerClient.TryParseMessage(raw) ?? "Unauthorized.", raw);
        }

        content = body is null ? null : JsonContent.Create(body, options: LicenseServerClient.JsonOptions);
        using (var req = BuildRequest(method, path, content, query, authorize))
        {
            return await _http.SendAsync(req, HttpCompletionOption.ResponseHeadersRead, ct).ConfigureAwait(false);
        }
    }

    private HttpRequestMessage BuildRequest(HttpMethod method, string path, HttpContent? content, IReadOnlyDictionary<string, string?>? query, bool authorize)
    {
        var url = BuildRequestUrl(path, query);
        var req = new HttpRequestMessage(method, url) { Content = content };

        if (authorize)
        {
            var session = Sessions.GetSession();
            if (!string.IsNullOrWhiteSpace(session?.AccessToken))
            {
                req.Headers.Authorization = new AuthenticationHeaderValue(session.TokenType.Trim().Length == 0 ? "Bearer" : session.TokenType, session.AccessToken);
            }
        }

        return req;
    }

    private string BuildRequestUrl(string path, IReadOnlyDictionary<string, string?>? query)
    {
        var url = Uri.TryCreate(path, UriKind.Absolute, out _) ? path : BaseUrl + "/api/client" + (path.StartsWith('/') ? path : "/" + path);
        var queryText = BuildQuery(query);
        if (string.IsNullOrWhiteSpace(queryText))
        {
            return url;
        }

        return url.Contains('?', StringComparison.Ordinal) ? url + "&" + queryText[1..] : url + queryText;
    }

    private async Task<bool> TryRefreshSessionAsync(CancellationToken ct)
    {
        var session = Sessions.GetSession();
        if (session is null || session.IsRefreshExpired(TokenRefreshSkew))
        {
            return false;
        }

        try
        {
            await RefreshSessionAsync(ct).ConfigureAwait(false);
            return true;
        }
        catch
        {
            return false;
        }
    }

    private void SaveSession(ClientLicenseInfo info, string? email)
    {
        if (string.IsNullOrWhiteSpace(info.AccessToken) || string.IsNullOrWhiteSpace(info.RefreshToken))
        {
            return;
        }

        var session = new ClientSession
        {
            BaseUrl = BaseUrl,
            AppKey = AppKey,
            MachineId = MachineId,
            AccessToken = info.AccessToken,
            RefreshToken = info.RefreshToken,
            TokenType = string.IsNullOrWhiteSpace(info.TokenType) ? "Bearer" : info.TokenType,
            AccessExpiresAt = info.AccessExpiresAt,
            RefreshExpiresAt = info.RefreshExpiresAt,
            SessionId = info.SessionId,
            AuthMode = info.AuthMode,
            DeviceId = info.DeviceId,
            CustomerId = info.CustomerId,
            Email = email ?? info.Email,
        };
        Sessions.Save(session);
    }

    private static void ApplyTokenResponse(ClientSession session, ClientSessionTokenResponse tokens)
    {
        session.AccessToken = tokens.AccessToken;
        session.RefreshToken = tokens.RefreshToken;
        session.TokenType = string.IsNullOrWhiteSpace(tokens.TokenType) ? "Bearer" : tokens.TokenType;
        session.AccessExpiresAt = tokens.AccessExpiresAt;
        session.RefreshExpiresAt = tokens.RefreshExpiresAt;
        session.SessionId = string.IsNullOrWhiteSpace(tokens.SessionId) ? session.SessionId : tokens.SessionId;
        session.AuthMode = string.IsNullOrWhiteSpace(tokens.AuthMode) ? session.AuthMode : tokens.AuthMode;
        session.DeviceId = string.IsNullOrWhiteSpace(tokens.DeviceId) ? session.DeviceId : tokens.DeviceId;
    }

    private static string NormalizeBaseUrl(string baseUrl)
        => baseUrl.Trim().TrimEnd('/');

    private static string CreateDefaultMachineId()
    {
        var raw = $"{Environment.MachineName}:{Environment.UserName}";
        var hash = SHA256.HashData(Encoding.UTF8.GetBytes(raw));
        return Convert.ToHexString(hash).ToLowerInvariant()[..32];
    }

    private static void AddUserAgent(HttpClient http)
    {
        try
        {
            http.DefaultRequestHeaders.UserAgent.Add(new ProductInfoHeaderValue("LicenseServerDotNetClientSdk", "1.0"));
        }
        catch
        {
        }
    }

    public void Dispose()
    {
        if (_disposed)
        {
            return;
        }

        _disposed = true;
        if (_disposeHttpClient)
        {
            _http.Dispose();
        }
    }
}

internal sealed class BufferedHttpContent
{
    private readonly byte[] _body;
    private readonly List<KeyValuePair<string, string[]>> _headers;

    private BufferedHttpContent(byte[] body, List<KeyValuePair<string, string[]>> headers)
    {
        _body = body;
        _headers = headers;
    }

    public static async Task<BufferedHttpContent> CreateAsync(HttpContent content, CancellationToken ct)
    {
        var body = await content.ReadAsByteArrayAsync(ct).ConfigureAwait(false);
        var headers = content.Headers
            .Select(static h => new KeyValuePair<string, string[]>(h.Key, h.Value.ToArray()))
            .ToList();
        return new BufferedHttpContent(body, headers);
    }

    public HttpContent ToHttpContent()
    {
        var content = new ByteArrayContent(_body);
        foreach (var header in _headers)
        {
            content.Headers.TryAddWithoutValidation(header.Key, header.Value);
        }

        return content;
    }
}

public sealed class ClientDeviceInfo
{
    [JsonPropertyName("name")]
    public string Name { get; set; } = string.Empty;

    [JsonPropertyName("hostname")]
    public string Hostname { get; set; } = string.Empty;

    [JsonPropertyName("os")]
    public string OS { get; set; } = string.Empty;

    [JsonPropertyName("os_version")]
    public string OSVersion { get; set; } = string.Empty;

    [JsonPropertyName("app_version")]
    public string AppVersion { get; set; } = string.Empty;
}

public sealed class ClientLicenseInfo
{
    [JsonPropertyName("valid")]
    public bool Valid { get; set; }

    [JsonPropertyName("license_id")]
    public string LicenseId { get; set; } = string.Empty;

    [JsonPropertyName("subscription_id")]
    public string SubscriptionId { get; set; } = string.Empty;

    [JsonPropertyName("customer_id")]
    public string CustomerId { get; set; } = string.Empty;

    [JsonPropertyName("device_id")]
    public string DeviceId { get; set; } = string.Empty;

    [JsonPropertyName("type")]
    public string Type { get; set; } = string.Empty;

    [JsonPropertyName("plan_type")]
    public string PlanType { get; set; } = string.Empty;

    [JsonPropertyName("expire_at")]
    public DateTime? ExpireAt { get; set; }

    [JsonPropertyName("remaining_days")]
    public int RemainingDays { get; set; }

    [JsonPropertyName("features")]
    public List<string> Features { get; set; } = [];

    [JsonPropertyName("signature")]
    public string Signature { get; set; } = string.Empty;

    [JsonPropertyName("access_token")]
    public string AccessToken { get; set; } = string.Empty;

    [JsonPropertyName("refresh_token")]
    public string RefreshToken { get; set; } = string.Empty;

    [JsonPropertyName("token_type")]
    public string TokenType { get; set; } = "Bearer";

    [JsonPropertyName("expires_in")]
    public int ExpiresIn { get; set; }

    [JsonPropertyName("refresh_expires_in")]
    public int RefreshExpiresIn { get; set; }

    [JsonPropertyName("access_expires_at")]
    public long AccessExpiresAt { get; set; }

    [JsonPropertyName("refresh_expires_at")]
    public long RefreshExpiresAt { get; set; }

    [JsonPropertyName("session_id")]
    public string SessionId { get; set; } = string.Empty;

    [JsonPropertyName("auth_mode")]
    public string AuthMode { get; set; } = string.Empty;

    [JsonPropertyName("email")]
    public string Email { get; set; } = string.Empty;
}

public sealed class ClientRegisterResult
{
    [JsonPropertyName("customer_id")]
    public string CustomerId { get; set; } = string.Empty;

    [JsonPropertyName("email")]
    public string Email { get; set; } = string.Empty;

    [JsonPropertyName("subscription_id")]
    public string SubscriptionId { get; set; } = string.Empty;

    [JsonPropertyName("plan_type")]
    public string PlanType { get; set; } = string.Empty;
}

public sealed class ClientHeartbeatResult
{
    [JsonPropertyName("valid")]
    public bool Valid { get; set; }

    [JsonPropertyName("remaining_days")]
    public int RemainingDays { get; set; }
}

internal sealed class ClientSessionTokenResponse
{
    [JsonPropertyName("access_token")]
    public string AccessToken { get; set; } = string.Empty;

    [JsonPropertyName("refresh_token")]
    public string RefreshToken { get; set; } = string.Empty;

    [JsonPropertyName("token_type")]
    public string TokenType { get; set; } = "Bearer";

    [JsonPropertyName("access_expires_at")]
    public long AccessExpiresAt { get; set; }

    [JsonPropertyName("refresh_expires_at")]
    public long RefreshExpiresAt { get; set; }

    [JsonPropertyName("session_id")]
    public string SessionId { get; set; } = string.Empty;

    [JsonPropertyName("auth_mode")]
    public string AuthMode { get; set; } = string.Empty;

    [JsonPropertyName("device_id")]
    public string DeviceId { get; set; } = string.Empty;
}
