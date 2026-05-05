using System.Net;
using System.Net.Http.Headers;
using System.Net.Http.Json;
using System.Text.Json;
using System.Text.Json.Serialization;

namespace LicenseServer.Sdk;

/// <summary>
/// Lightweight HTTP client for license-server.
/// </summary>
public sealed class LicenseServerClient : IDisposable
{
    internal static readonly JsonSerializerOptions JsonOptions = new(JsonSerializerDefaults.Web)
    {
        DefaultIgnoreCondition = JsonIgnoreCondition.WhenWritingNull,
    };

    private readonly HttpClient _http;
    private readonly bool _disposeHttpClient;
    private bool _disposed;
    private string? _baseUrl;

    public LicenseServerClient(
        string? baseUrl = null,
        IAuthSessionStore? sessions = null,
        HttpClient? httpClient = null,
        bool disposeHttpClient = false)
    {
        Sessions = sessions ?? new InMemoryAuthSessionStore();
        _http = httpClient ?? new HttpClient();
        _disposeHttpClient = httpClient is null || disposeHttpClient;
        _http.DefaultRequestHeaders.UserAgent.Add(new ProductInfoHeaderValue("LicenseServerSdk", "1.0"));
        SetBaseUrl(baseUrl);
    }

    public IAuthSessionStore Sessions { get; }

    public string? BaseUrl => _baseUrl;

    public string? CurrentBaseUrl => _baseUrl ?? Sessions.GetSession()?.BaseUrl;

    public void SetBaseUrl(string? baseUrl)
        => _baseUrl = string.IsNullOrWhiteSpace(baseUrl) ? null : baseUrl.TrimEnd('/');

    public Task<T> GetAsync<T>(string path, CancellationToken ct = default)
        => SendJsonAsync<T>(HttpMethod.Get, path, body: null, ct);

    public Task<T> PostAsync<T>(string path, object? body, CancellationToken ct = default)
        => SendJsonAsync<T>(HttpMethod.Post, path, body, ct);

    public Task<T> PutAsync<T>(string path, object? body, CancellationToken ct = default)
        => SendJsonAsync<T>(HttpMethod.Put, path, body, ct);

    public Task<T> DeleteAsync<T>(string path, CancellationToken ct = default)
        => SendJsonAsync<T>(HttpMethod.Delete, path, body: null, ct);

    public async Task<HttpResponseMessage> SendRawAsync(HttpMethod method, string path, HttpContent? content, CancellationToken ct = default)
    {
        using var req = BuildRequest(method, path, content);
        var resp = await _http.SendAsync(req, HttpCompletionOption.ResponseHeadersRead, ct).ConfigureAwait(false);
        if (resp.StatusCode == HttpStatusCode.Unauthorized)
        {
            Sessions.Clear();
            var body = await SafeReadAsync(resp, ct).ConfigureAwait(false);
            resp.Dispose();
            throw new LicenseServerException(HttpStatusCode.Unauthorized, TryParseBusinessCode(body), TryParseMessage(body) ?? "Unauthorized.", body);
        }

        return resp;
    }

    internal static int TryParseBusinessCode(string body)
    {
        try
        {
            using var doc = JsonDocument.Parse(body);
            if (doc.RootElement.TryGetProperty("code", out var code) && code.ValueKind == JsonValueKind.Number)
            {
                return code.GetInt32();
            }
        }
        catch
        {
        }

        return 0;
    }

    internal static string? TryParseMessage(string body)
    {
        try
        {
            using var doc = JsonDocument.Parse(body);
            if (doc.RootElement.TryGetProperty("message", out var message))
            {
                return message.GetString();
            }
        }
        catch
        {
        }

        return null;
    }

    internal static async Task<string> SafeReadAsync(HttpResponseMessage resp, CancellationToken ct = default)
    {
        try
        {
            return await resp.Content.ReadAsStringAsync(ct).ConfigureAwait(false);
        }
        catch
        {
            return string.Empty;
        }
    }

    private async Task<T> SendJsonAsync<T>(HttpMethod method, string path, object? body, CancellationToken ct)
    {
        HttpResponseMessage? resp = null;
        try
        {
            var content = body is null ? null : JsonContent.Create(body, options: JsonOptions);
            resp = await SendRawAsync(method, path, content, ct).ConfigureAwait(false);
            var raw = await SafeReadAsync(resp, ct).ConfigureAwait(false);

            if (!resp.IsSuccessStatusCode)
            {
                throw new LicenseServerException(resp.StatusCode, TryParseBusinessCode(raw), TryParseMessage(raw) ?? $"HTTP {(int)resp.StatusCode}", raw);
            }

            var parsed = JsonSerializer.Deserialize<ApiResponse<T>>(raw, JsonOptions)
                ?? throw new LicenseServerException(resp.StatusCode, 0, "Response cannot be parsed.", raw);
            if (parsed.Code != 0)
            {
                throw new LicenseServerException(resp.StatusCode, parsed.Code, parsed.Message ?? "Unknown server error.", raw);
            }

            return parsed.Data
                ?? throw new LicenseServerException(resp.StatusCode, 0, "Response data is empty.", raw);
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

    private HttpRequestMessage BuildRequest(HttpMethod method, string path, HttpContent? content)
    {
        var baseUrl = CurrentBaseUrl
            ?? throw new InvalidOperationException("LicenseServerClient has no BaseUrl. Set BaseUrl or login first.");
        if (!path.StartsWith('/'))
        {
            path = "/" + path;
        }

        var req = new HttpRequestMessage(method, baseUrl + path) { Content = content };
        var session = Sessions.GetSession();
        if (!string.IsNullOrWhiteSpace(session?.Token))
        {
            req.Headers.Authorization = new AuthenticationHeaderValue("Bearer", session.Token);
        }

        return req;
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
