using System.Security.Cryptography;
using System.Text;
using System.Text.Json.Serialization;

namespace LicenseServer.Sdk;

/// <summary>
/// Secure script endpoints under /api/client/secure-scripts.
/// </summary>
public sealed class SecureScriptApi
{
    private readonly LicenseClient _client;
    private readonly Dictionary<string, CachedSecureScript> _cache = [];
    private readonly SemaphoreSlim _cacheLock = new(1, 1);

    internal SecureScriptApi(LicenseClient client)
    {
        _client = client;
    }

    public string AppSecret { get; set; } = string.Empty;

    public RSA? SignaturePublicKey { get; set; }

    public void SetSignaturePublicKeyPem(string publicKeyPem)
    {
        LicenseClient.EnsureNotEmpty(publicKeyPem, nameof(publicKeyPem));
        var rsa = RSA.Create();
        rsa.ImportFromPem(publicKeyPem);
        SignaturePublicKey = rsa;
    }

    public async Task<List<SecureScriptVersionInfo>> GetVersionsAsync(CancellationToken ct = default)
        => await _client.GetAsync<List<SecureScriptVersionInfo>>("/secure-scripts/versions", ct: ct).ConfigureAwait(false) ?? [];

    public async Task<CachedSecureScript> FetchAsync(string scriptId, bool force = false, CancellationToken ct = default)
    {
        LicenseClient.EnsureNotEmpty(scriptId, nameof(scriptId));

        if (!force)
        {
            await _cacheLock.WaitAsync(ct).ConfigureAwait(false);
            try
            {
                if (_cache.TryGetValue(scriptId, out var cached) && DateTimeOffset.UtcNow < cached.ExpiresAt)
                {
                    return cached;
                }
            }
            finally
            {
                _cacheLock.Release();
            }
        }

        var pkg = RequireData(await _client.PostAsync<EncryptedScriptPackage>("/secure-scripts/fetch", new
        {
            script_id = scriptId,
        }, ct: ct).ConfigureAwait(false), "Secure script package response data is empty.");

        if (DateTimeOffset.UtcNow.ToUnixTimeSeconds() > pkg.ExpiresAt)
        {
            throw new LicenseServerException("Secure script package is expired.", new InvalidDataException("Expired package."));
        }

        VerifyPackageSignature(pkg);
        var key = DeriveKey(pkg.KeyHint);
        var content = DecryptContent(pkg.EncryptedContent, key);
        var hash = Convert.ToHexString(SHA256.HashData(content)).ToLowerInvariant();
        if (!string.Equals(hash, pkg.ContentHash, StringComparison.OrdinalIgnoreCase))
        {
            throw new LicenseServerException("Secure script content hash mismatch.", new InvalidDataException("SHA-256 mismatch."));
        }

        var result = new CachedSecureScript
        {
            ScriptId = pkg.ScriptId,
            DeliveryId = pkg.DeliveryId,
            Version = pkg.Version,
            Content = content,
            ContentHash = pkg.ContentHash,
            FetchedAt = DateTimeOffset.UtcNow,
            ExpiresAt = DateTimeOffset.FromUnixTimeSeconds(pkg.ExpiresAt),
        };

        await _cacheLock.WaitAsync(ct).ConfigureAwait(false);
        try
        {
            _cache[scriptId] = result;
        }
        finally
        {
            _cacheLock.Release();
        }

        return result;
    }

    public async Task<string> ExecuteAsync(string scriptId, Func<byte[], IReadOnlyDictionary<string, object?>, Task<string>> executor, IReadOnlyDictionary<string, object?>? args = null, CancellationToken ct = default)
    {
        ArgumentNullException.ThrowIfNull(executor);

        CachedSecureScript script;
        try
        {
            script = await FetchAsync(scriptId, ct: ct).ConfigureAwait(false);
        }
        catch (Exception ex)
        {
            await ReportAsync(scriptId, string.Empty, "failed", error: ex.Message, ct: CancellationToken.None).ConfigureAwait(false);
            throw;
        }

        await ReportAsync(scriptId, script.DeliveryId, "executing", ct: ct).ConfigureAwait(false);
        var started = DateTimeOffset.UtcNow;
        try
        {
            var result = await executor(script.Content, args ?? new Dictionary<string, object?>()).ConfigureAwait(false);
            var duration = (int)(DateTimeOffset.UtcNow - started).TotalMilliseconds;
            await ReportAsync(scriptId, script.DeliveryId, "success", result, duration: duration, ct: ct).ConfigureAwait(false);
            await RemoveCachedAsync(scriptId, ct).ConfigureAwait(false);
            return result;
        }
        catch (Exception ex)
        {
            var duration = (int)(DateTimeOffset.UtcNow - started).TotalMilliseconds;
            await ReportAsync(scriptId, script.DeliveryId, "failed", error: ex.Message, duration: duration, ct: CancellationToken.None).ConfigureAwait(false);
            await RemoveCachedAsync(scriptId, CancellationToken.None).ConfigureAwait(false);
            throw;
        }
    }

    public async Task ReportAsync(string scriptId, string deliveryId, string status, string? result = null, string? error = null, int duration = 0, CancellationToken ct = default)
    {
        await _client.SendNoDataAsync(HttpMethod.Post, "/secure-scripts/report", new
        {
            script_id = scriptId,
            delivery_id = deliveryId,
            status,
            result,
            error_message = error,
            duration,
        }, ct: ct).ConfigureAwait(false);
    }

    public async Task ClearCacheAsync(CancellationToken ct = default)
    {
        await _cacheLock.WaitAsync(ct).ConfigureAwait(false);
        try
        {
            _cache.Clear();
        }
        finally
        {
            _cacheLock.Release();
        }
    }

    private async Task RemoveCachedAsync(string scriptId, CancellationToken ct)
    {
        await _cacheLock.WaitAsync(ct).ConfigureAwait(false);
        try
        {
            _cache.Remove(scriptId);
        }
        finally
        {
            _cacheLock.Release();
        }
    }

    private byte[] DeriveKey(string keyHint)
    {
        if (string.IsNullOrWhiteSpace(AppSecret))
        {
            throw new LicenseServerException("Secure script app secret is not configured.", new InvalidOperationException("Missing app secret."));
        }

        return HKDF.DeriveKey(HashAlgorithmName.SHA256, Encoding.UTF8.GetBytes(AppSecret), 32, Encoding.UTF8.GetBytes(_client.MachineId), Encoding.UTF8.GetBytes(keyHint));
    }

    private static byte[] DecryptContent(string encryptedContent, byte[] key)
    {
        var raw = Convert.FromBase64String(encryptedContent);
        if (raw.Length < 12 + 16)
        {
            throw new LicenseServerException("Secure script ciphertext is invalid.", new InvalidDataException("Ciphertext too short."));
        }

        var nonce = raw.AsSpan(0, 12);
        var cipherAndTag = raw.AsSpan(12);
        var ciphertext = cipherAndTag[..^16];
        var tag = cipherAndTag[^16..];
        var plaintext = new byte[ciphertext.Length];
        using var aes = new AesGcm(key, 16);
        aes.Decrypt(nonce, ciphertext, tag, plaintext);
        return plaintext;
    }

    private void VerifyPackageSignature(EncryptedScriptPackage pkg)
    {
        if (SignaturePublicKey is null)
        {
            return;
        }

        var payload = Encoding.UTF8.GetBytes($"{pkg.ScriptId}:{pkg.EncryptedContent}:{_client.MachineId}:{pkg.ExpiresAt}");
        var signature = Convert.FromBase64String(pkg.Signature);
        if (!SignaturePublicKey.VerifyData(payload, signature, HashAlgorithmName.SHA256, RSASignaturePadding.Pkcs1))
        {
            throw new LicenseServerException("Secure script signature verification failed.", new InvalidDataException("Invalid signature."));
        }
    }

    private static T RequireData<T>(T? value, string message)
        where T : class
        => value ?? throw new LicenseServerException(message, new InvalidDataException("Response data is empty."));
}

public sealed class CachedSecureScript
{
    public string ScriptId { get; set; } = string.Empty;

    public string DeliveryId { get; set; } = string.Empty;

    public string Version { get; set; } = string.Empty;

    public byte[] Content { get; set; } = [];

    public string ContentHash { get; set; } = string.Empty;

    public DateTimeOffset FetchedAt { get; set; }

    public DateTimeOffset ExpiresAt { get; set; }
}

public sealed class SecureScriptVersionInfo
{
    [JsonPropertyName("script_id")]
    public string ScriptId { get; set; } = string.Empty;

    [JsonPropertyName("name")]
    public string Name { get; set; } = string.Empty;

    [JsonPropertyName("version")]
    public string Version { get; set; } = string.Empty;

    [JsonPropertyName("content_hash")]
    public string ContentHash { get; set; } = string.Empty;

    [JsonPropertyName("updated_at")]
    public long UpdatedAt { get; set; }
}

public sealed class EncryptedScriptPackage
{
    [JsonPropertyName("script_id")]
    public string ScriptId { get; set; } = string.Empty;

    [JsonPropertyName("delivery_id")]
    public string DeliveryId { get; set; } = string.Empty;

    [JsonPropertyName("version")]
    public string Version { get; set; } = string.Empty;

    [JsonPropertyName("script_type")]
    public string ScriptType { get; set; } = string.Empty;

    [JsonPropertyName("entry_point")]
    public string EntryPoint { get; set; } = string.Empty;

    [JsonPropertyName("encrypted_content")]
    public string EncryptedContent { get; set; } = string.Empty;

    [JsonPropertyName("content_hash")]
    public string ContentHash { get; set; } = string.Empty;

    [JsonPropertyName("key_hint")]
    public string KeyHint { get; set; } = string.Empty;

    [JsonPropertyName("signature")]
    public string Signature { get; set; } = string.Empty;

    [JsonPropertyName("expires_at")]
    public long ExpiresAt { get; set; }

    [JsonPropertyName("timeout")]
    public int Timeout { get; set; }

    [JsonPropertyName("memory_limit")]
    public int MemoryLimit { get; set; }

    [JsonPropertyName("parameters")]
    public string Parameters { get; set; } = string.Empty;

    [JsonPropertyName("execute_once")]
    public bool ExecuteOnce { get; set; }
}
