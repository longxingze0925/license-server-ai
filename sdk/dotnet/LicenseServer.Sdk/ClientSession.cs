using System.Text.Json.Serialization;

namespace LicenseServer.Sdk;

/// <summary>
/// Client-side app session issued by /api/client/auth/* endpoints.
/// </summary>
public sealed class ClientSession
{
    [JsonPropertyName("base_url")]
    public string BaseUrl { get; set; } = string.Empty;

    [JsonPropertyName("app_key")]
    public string AppKey { get; set; } = string.Empty;

    [JsonPropertyName("machine_id")]
    public string MachineId { get; set; } = string.Empty;

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

    [JsonPropertyName("customer_id")]
    public string CustomerId { get; set; } = string.Empty;

    [JsonPropertyName("email")]
    public string Email { get; set; } = string.Empty;

    public bool IsAccessExpired(TimeSpan skew)
        => string.IsNullOrWhiteSpace(AccessToken) ||
           (AccessExpiresAt > 0 && DateTimeOffset.UtcNow.Add(skew).ToUnixTimeSeconds() >= AccessExpiresAt);

    public bool IsRefreshExpired(TimeSpan skew)
        => string.IsNullOrWhiteSpace(RefreshToken) ||
           (RefreshExpiresAt > 0 && DateTimeOffset.UtcNow.Add(skew).ToUnixTimeSeconds() >= RefreshExpiresAt);
}
