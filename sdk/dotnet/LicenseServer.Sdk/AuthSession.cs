using System.Text.Json.Serialization;

namespace LicenseServer.Sdk;

/// <summary>
/// Authenticated backend session. Applications may persist this with their own secure store.
/// </summary>
public sealed class AuthSession
{
    [JsonPropertyName("base_url")]
    public string BaseUrl { get; set; } = string.Empty;

    [JsonPropertyName("token")]
    public string Token { get; set; } = string.Empty;

    [JsonPropertyName("user_id")]
    public string UserId { get; set; } = string.Empty;

    [JsonPropertyName("email")]
    public string Email { get; set; } = string.Empty;

    [JsonPropertyName("name")]
    public string Name { get; set; } = string.Empty;

    [JsonPropertyName("role")]
    public string Role { get; set; } = string.Empty;

    [JsonPropertyName("tenant_id")]
    public string TenantId { get; set; } = string.Empty;

    [JsonPropertyName("expires_at_utc")]
    public DateTime? ExpiresAtUtc { get; set; }

    public bool IsExpired(TimeSpan skew)
    {
        if (string.IsNullOrWhiteSpace(Token))
        {
            return true;
        }

        return ExpiresAtUtc is not null && DateTime.UtcNow + skew >= ExpiresAtUtc.Value;
    }
}
