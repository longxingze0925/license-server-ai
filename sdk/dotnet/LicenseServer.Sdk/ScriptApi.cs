using System.Security.Cryptography;
using System.Text;
using System.Text.Json.Serialization;
using System.Net;

namespace LicenseServer.Sdk;

/// <summary>
/// Plain script and release-download endpoints under /api/client.
/// </summary>
public sealed class ScriptApi
{
    private readonly LicenseClient _client;

    internal ScriptApi(LicenseClient client)
    {
        _client = client;
    }

    public RSA? SignaturePublicKey { get; set; }

    public bool RequireSignature { get; set; }

    public void SetSignaturePublicKeyPem(string publicKeyPem)
    {
        LicenseClient.EnsureNotEmpty(publicKeyPem, nameof(publicKeyPem));
        var rsa = RSA.Create();
        rsa.ImportFromPem(publicKeyPem);
        SignaturePublicKey = rsa;
    }

    public async Task<ScriptVersionResponse> GetScriptVersionsAsync(CancellationToken ct = default)
    {
        var result = await _client.GetAsync<ScriptVersionResponse>("/scripts/version", ct: ct).ConfigureAwait(false) ?? new ScriptVersionResponse();
        result.Scripts ??= [];
        return result;
    }

    public async Task<byte[]> DownloadScriptAsync(string filename, string? destinationPath = null, CancellationToken ct = default)
    {
        LicenseClient.EnsureNotEmpty(filename, nameof(filename));

        using var resp = await _client.SendRawAsync(HttpMethod.Get, "/scripts/" + Uri.EscapeDataString(filename), content: null, ct: ct).ConfigureAwait(false);
        var content = await ReadDownloadResponseAsync(resp, ct).ConfigureAwait(false);
        if (!string.IsNullOrWhiteSpace(destinationPath))
        {
            var fullPath = Path.GetFullPath(destinationPath);
            var dir = Path.GetDirectoryName(fullPath);
            if (!string.IsNullOrWhiteSpace(dir))
            {
                Directory.CreateDirectory(dir);
            }
            await File.WriteAllBytesAsync(fullPath, content, ct).ConfigureAwait(false);
        }
        return content;
    }

    public async Task<ReleaseInfo> GetLatestReleaseAsync(CancellationToken ct = default)
        => RequireData(await _client.GetAsync<ReleaseInfo>("/releases/latest", ct: ct).ConfigureAwait(false), "Latest release response data is empty.");

    public async Task<long> DownloadReleaseAsync(string filename, string destinationPath, IProgress<DownloadProgress>? progress = null, CancellationToken ct = default)
    {
        LicenseClient.EnsureNotEmpty(filename, nameof(filename));
        using var resp = await _client.SendRawAsync(HttpMethod.Get, "/releases/download/" + Uri.EscapeDataString(filename), content: null, ct: ct).ConfigureAwait(false);
        return await SaveDownloadResponseAsync(resp, destinationPath, expected: null, progress, ct).ConfigureAwait(false);
    }

    public async Task<ReleaseInfo> GetLatestReleaseAndDownloadAsync(string destinationPath, IProgress<DownloadProgress>? progress = null, CancellationToken ct = default)
    {
        var release = await GetLatestReleaseAsync(ct).ConfigureAwait(false);
        await DownloadReleaseUrlAsync(release, destinationPath, progress, ct).ConfigureAwait(false);
        return release;
    }

    private async Task<long> DownloadReleaseUrlAsync(ReleaseInfo release, string destinationPath, IProgress<DownloadProgress>? progress, CancellationToken ct)
    {
        if (string.IsNullOrWhiteSpace(release.DownloadUrl))
        {
            throw new InvalidOperationException("Release has no download URL.");
        }

        try
        {
            using var resp = await _client.SendRawAsync(HttpMethod.Get, _client.BuildAbsoluteUrl(release.DownloadUrl), content: null, authorize: false, allowRefresh: false, ct: ct).ConfigureAwait(false);
            return await SaveDownloadResponseAsync(resp, destinationPath, release, progress, ct).ConfigureAwait(false);
        }
        catch (LicenseServerException ex) when (ex.IsUnauthorized || ex.HttpStatus == HttpStatusCode.Forbidden)
        {
            var refreshed = await GetLatestReleaseAsync(ct).ConfigureAwait(false);
            if (string.IsNullOrWhiteSpace(refreshed.DownloadUrl))
            {
                throw new LicenseServerException("Release download URL expired and no fresh URL is available.", ex);
            }

            CopyReleaseInfo(refreshed, release);
            using var retryResp = await _client.SendRawAsync(HttpMethod.Get, _client.BuildAbsoluteUrl(release.DownloadUrl), content: null, authorize: false, allowRefresh: false, ct: ct).ConfigureAwait(false);
            return await SaveDownloadResponseAsync(retryResp, destinationPath, release, progress, ct).ConfigureAwait(false);
        }
    }

    private static async Task<byte[]> ReadDownloadResponseAsync(HttpResponseMessage resp, CancellationToken ct)
    {
        if (!resp.IsSuccessStatusCode)
        {
            var raw = await LicenseServerClient.SafeReadAsync(resp, ct).ConfigureAwait(false);
            throw new LicenseServerException(resp.StatusCode, LicenseServerClient.TryParseBusinessCode(raw), LicenseServerClient.TryParseMessage(raw) ?? $"download failed: HTTP {(int)resp.StatusCode}", raw);
        }
        return await resp.Content.ReadAsByteArrayAsync(ct).ConfigureAwait(false);
    }

    private async Task<long> SaveDownloadResponseAsync(HttpResponseMessage resp, string destinationPath, ReleaseInfo? expected, IProgress<DownloadProgress>? progress, CancellationToken ct)
    {
        if (!resp.IsSuccessStatusCode)
        {
            var raw = await LicenseServerClient.SafeReadAsync(resp, ct).ConfigureAwait(false);
            throw new LicenseServerException(resp.StatusCode, LicenseServerClient.TryParseBusinessCode(raw), LicenseServerClient.TryParseMessage(raw) ?? $"download failed: HTTP {(int)resp.StatusCode}", raw);
        }

        var fullPath = Path.GetFullPath(destinationPath);
        var dir = Path.GetDirectoryName(fullPath);
        if (!string.IsNullOrWhiteSpace(dir))
        {
            Directory.CreateDirectory(dir);
        }

        var tempPath = fullPath + ".part";
        try
        {
            var total = resp.Content.Headers.ContentLength ?? 0;
            long written = 0;
            await using (var input = await resp.Content.ReadAsStreamAsync(ct).ConfigureAwait(false))
            await using (var output = new FileStream(tempPath, FileMode.Create, FileAccess.Write, FileShare.None, 81920, useAsync: true))
            using (var sha = SHA256.Create())
            {
                var buffer = new byte[81920];
                while (true)
                {
                    var read = await input.ReadAsync(buffer, ct).ConfigureAwait(false);
                    if (read == 0)
                    {
                        break;
                    }
                    await output.WriteAsync(buffer.AsMemory(0, read), ct).ConfigureAwait(false);
                    sha.TransformBlock(buffer, 0, read, null, 0);
                    written += read;
                    progress?.Report(new DownloadProgress(written, total, total > 0 ? (double)written / total : 0));
                }

                sha.TransformFinalBlock([], 0, 0);
                var actualHash = Convert.ToHexString(sha.Hash ?? []).ToLowerInvariant();
                if (expected is not null)
                {
                    if (!string.IsNullOrWhiteSpace(expected.FileHash) && !string.Equals(actualHash, expected.FileHash, StringComparison.OrdinalIgnoreCase))
                    {
                        throw new LicenseServerException("Downloaded release hash mismatch.", new InvalidDataException("SHA-256 mismatch."));
                    }
                    VerifyFileSignature(actualHash, written, expected.FileSignature, expected.SignatureAlg);
                }
            }

            if (File.Exists(fullPath))
            {
                File.Delete(fullPath);
            }
            File.Move(tempPath, fullPath);
            return written;
        }
        catch
        {
            TryDeleteFile(tempPath);
            throw;
        }
    }

    private static void CopyReleaseInfo(ReleaseInfo source, ReleaseInfo target)
    {
        target.Version = source.Version;
        target.VersionCode = source.VersionCode;
        target.DownloadUrl = source.DownloadUrl;
        target.Changelog = source.Changelog;
        target.FileSize = source.FileSize;
        target.FileHash = source.FileHash;
        target.FileSignature = source.FileSignature;
        target.SignatureAlg = source.SignatureAlg;
        target.ForceUpdate = source.ForceUpdate;
    }

    private static void TryDeleteFile(string path)
    {
        try
        {
            if (File.Exists(path))
            {
                File.Delete(path);
            }
        }
        catch
        {
        }
    }

    private void VerifyFileSignature(string fileHash, long fileSize, string fileSignature, string signatureAlg)
    {
        if (string.IsNullOrWhiteSpace(fileSignature))
        {
            if (RequireSignature)
            {
                throw new LicenseServerException("Downloaded release signature is missing.", new InvalidDataException("Signature missing."));
            }
            return;
        }

        if (!string.IsNullOrWhiteSpace(signatureAlg) && !string.Equals(signatureAlg, "RSA-SHA256", StringComparison.OrdinalIgnoreCase))
        {
            throw new LicenseServerException("Unsupported release signature algorithm: " + signatureAlg, new InvalidDataException("Unsupported signature algorithm."));
        }

        if (SignaturePublicKey is null)
        {
            if (RequireSignature)
            {
                throw new LicenseServerException("Release public key is not configured.", new InvalidOperationException("Missing public key."));
            }
            return;
        }

        var payload = Encoding.UTF8.GetBytes(fileHash.Trim().ToLowerInvariant() + ":" + fileSize);
        var signature = Convert.FromBase64String(fileSignature);
        if (!SignaturePublicKey.VerifyData(payload, signature, HashAlgorithmName.SHA256, RSASignaturePadding.Pkcs1))
        {
            throw new LicenseServerException("Downloaded release signature verification failed.", new InvalidDataException("Invalid signature."));
        }
    }

    private static T RequireData<T>(T? value, string message)
        where T : class
        => value ?? throw new LicenseServerException(message, new InvalidDataException("Response data is empty."));
}

public sealed class ScriptVersionResponse
{
    [JsonPropertyName("scripts")]
    public List<ScriptInfo> Scripts { get; set; } = [];

    [JsonPropertyName("total_count")]
    public int TotalCount { get; set; }

    [JsonPropertyName("last_updated")]
    public string LastUpdated { get; set; } = string.Empty;
}

public sealed class ScriptInfo
{
    [JsonPropertyName("filename")]
    public string Filename { get; set; } = string.Empty;

    [JsonPropertyName("version")]
    public string Version { get; set; } = string.Empty;

    [JsonPropertyName("version_code")]
    public int VersionCode { get; set; }

    [JsonPropertyName("file_size")]
    public long FileSize { get; set; }

    [JsonPropertyName("file_hash")]
    public string FileHash { get; set; } = string.Empty;

    [JsonPropertyName("updated_at")]
    public string UpdatedAt { get; set; } = string.Empty;
}

public sealed class ReleaseInfo
{
    [JsonPropertyName("version")]
    public string Version { get; set; } = string.Empty;

    [JsonPropertyName("version_code")]
    public int VersionCode { get; set; }

    [JsonPropertyName("download_url")]
    public string DownloadUrl { get; set; } = string.Empty;

    [JsonPropertyName("changelog")]
    public string Changelog { get; set; } = string.Empty;

    [JsonPropertyName("file_size")]
    public long FileSize { get; set; }

    [JsonPropertyName("file_hash")]
    public string FileHash { get; set; } = string.Empty;

    [JsonPropertyName("file_signature")]
    public string FileSignature { get; set; } = string.Empty;

    [JsonPropertyName("signature_alg")]
    public string SignatureAlg { get; set; } = string.Empty;

    [JsonPropertyName("force_update")]
    public bool ForceUpdate { get; set; }
}
