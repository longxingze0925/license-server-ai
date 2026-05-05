using System.Text.Json;
using System.Text.Json.Serialization;

namespace LicenseServer.Sdk;

/// <summary>
/// Admin/user JWT auth API used by the backend proxy.
/// </summary>
public sealed class AuthApi
{
    private readonly LicenseServerClient _client;

    public AuthApi(LicenseServerClient client)
    {
        _client = client;
    }

    public async Task<AuthSession> LoginAsync(string baseUrl, string email, string password, CancellationToken ct = default)
    {
        if (string.IsNullOrWhiteSpace(baseUrl))
        {
            throw new ArgumentException("Base URL cannot be empty.", nameof(baseUrl));
        }
        if (string.IsNullOrWhiteSpace(email))
        {
            throw new ArgumentException("Email cannot be empty.", nameof(email));
        }
        if (string.IsNullOrWhiteSpace(password))
        {
            throw new ArgumentException("Password cannot be empty.", nameof(password));
        }

        var previousBaseUrl = _client.BaseUrl;
        var normalizedBaseUrl = baseUrl.TrimEnd('/');
        _client.SetBaseUrl(normalizedBaseUrl);

        try
        {
            var resp = await _client.PostAsync<LoginResponse>("/api/auth/login", new { email, password }, ct).ConfigureAwait(false);
            var session = new AuthSession
            {
                BaseUrl = normalizedBaseUrl,
                Token = resp.Token,
                UserId = resp.User?.Id ?? string.Empty,
                Email = resp.User?.Email ?? email,
                Name = resp.User?.Name ?? string.Empty,
                Role = resp.User?.Role ?? string.Empty,
                TenantId = resp.User?.TenantId ?? resp.Tenant?.Id ?? string.Empty,
                ExpiresAtUtc = ComputeExpiresAt(resp.ExpiresIn),
            };
            _client.Sessions.Save(session);
            return session;
        }
        catch
        {
            _client.SetBaseUrl(previousBaseUrl);
            throw;
        }
    }

    public async Task<UserProfile> GetProfileAsync(CancellationToken ct = default)
    {
        var data = await _client.GetAsync<JsonElement>("/api/auth/profile", ct).ConfigureAwait(false);
        if (!data.TryGetProperty("user", out var userElement))
        {
            return JsonSerializer.Deserialize<UserProfile>(data.GetRawText(), LicenseServerClient.JsonOptions)
                ?? new UserProfile();
        }

        var profile = JsonSerializer.Deserialize<UserProfile>(userElement.GetRawText(), LicenseServerClient.JsonOptions)
            ?? new UserProfile();
        if (string.IsNullOrWhiteSpace(profile.TenantId) &&
            data.TryGetProperty("tenant", out var tenantElement) &&
            tenantElement.TryGetProperty("id", out var tenantIdElement))
        {
            profile.TenantId = tenantIdElement.GetString() ?? string.Empty;
        }

        return profile;
    }

    public async Task<bool> RestoreAsync(CancellationToken ct = default)
    {
        if (_client.Sessions.GetSession() is null)
        {
            return false;
        }

        try
        {
            await GetProfileAsync(ct).ConfigureAwait(false);
            return true;
        }
        catch (LicenseServerException ex)
        {
            if (ex.IsUnauthorized)
            {
                _client.Sessions.Clear();
            }

            return false;
        }
    }

    public void Logout() => _client.Sessions.Clear();

    private static DateTime? ComputeExpiresAt(int? expiresInSeconds)
    {
        var seconds = expiresInSeconds is > 0 ? expiresInSeconds.Value : 24 * 3600;
        return DateTime.UtcNow.AddSeconds(seconds);
    }

    public sealed class LoginResponse
    {
        [JsonPropertyName("token")]
        public string Token { get; set; } = string.Empty;

        [JsonPropertyName("user")]
        public UserProfile? User { get; set; }

        [JsonPropertyName("tenant")]
        public TenantInfo? Tenant { get; set; }

        [JsonPropertyName("expires_in")]
        public int? ExpiresIn { get; set; }
    }

    public sealed class TenantInfo
    {
        [JsonPropertyName("id")]
        public string Id { get; set; } = string.Empty;

        [JsonPropertyName("name")]
        public string Name { get; set; } = string.Empty;

        [JsonPropertyName("slug")]
        public string Slug { get; set; } = string.Empty;

        [JsonPropertyName("plan")]
        public string Plan { get; set; } = string.Empty;
    }

    public sealed class UserProfile
    {
        [JsonPropertyName("id")]
        public string Id { get; set; } = string.Empty;

        [JsonPropertyName("email")]
        public string Email { get; set; } = string.Empty;

        [JsonPropertyName("name")]
        public string Name { get; set; } = string.Empty;

        [JsonPropertyName("role")]
        public string Role { get; set; } = string.Empty;

        [JsonPropertyName("tenant_id")]
        public string TenantId { get; set; } = string.Empty;
    }
}
