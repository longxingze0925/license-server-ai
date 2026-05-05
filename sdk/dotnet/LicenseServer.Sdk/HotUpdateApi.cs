using System.Security.Cryptography;
using System.Text;
using System.Text.Json.Serialization;

namespace LicenseServer.Sdk;

/// <summary>
/// Hot update endpoints under /api/client/hotupdate.
/// </summary>
public sealed class HotUpdateApi
{
    private readonly LicenseClient _client;

    internal HotUpdateApi(LicenseClient client)
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

    public async Task<HotUpdateInfo> CheckAsync(string currentVersion, CancellationToken ct = default)
    {
        LicenseClient.EnsureNotEmpty(currentVersion, nameof(currentVersion));
        return RequireData(await _client.GetAsync<HotUpdateInfo>("/hotupdate/check", new Dictionary<string, string?>
        {
            ["version"] = currentVersion.Trim(),
        }, ct).ConfigureAwait(false), "Hot update response data is empty.");
    }

    public async Task<List<HotUpdateHistoryItem>> GetHistoryAsync(CancellationToken ct = default)
        => await _client.GetAsync<List<HotUpdateHistoryItem>>("/hotupdate/history", ct: ct).ConfigureAwait(false) ?? [];

    public async Task ReportAsync(string hotUpdateId, string fromVersion, string status, string? errorMessage = null, string? toVersion = null, CancellationToken ct = default)
    {
        LicenseClient.EnsureNotEmpty(hotUpdateId, nameof(hotUpdateId));
        LicenseClient.EnsureNotEmpty(status, nameof(status));

        await _client.SendNoDataAsync(HttpMethod.Post, "/hotupdate/report", new
        {
            hot_update_id = hotUpdateId,
            from_version = fromVersion,
            to_version = toVersion,
            status,
            error_message = errorMessage,
        }, ct: ct).ConfigureAwait(false);
    }

    public async Task<long> DownloadAsync(HotUpdateInfo update, string destinationPath, IProgress<DownloadProgress>? progress = null, CancellationToken ct = default)
    {
        ArgumentNullException.ThrowIfNull(update);
        if (!update.HasUpdate)
        {
            throw new InvalidOperationException("No hot update is available.");
        }
        if (string.IsNullOrWhiteSpace(update.DownloadUrl))
        {
            throw new InvalidOperationException("Hot update has no download URL.");
        }

        await ReportAsync(update.Id, update.FromVersion, HotUpdateStatus.Downloading, ct: ct).ConfigureAwait(false);
        try
        {
            var written = await DownloadFileWithRefreshAsync(update, destinationPath, progress, ct).ConfigureAwait(false);
            progress?.Report(new DownloadProgress(written, update.FileSize, 1));
            return written;
        }
        catch (Exception ex)
        {
            await ReportAsync(update.Id, update.FromVersion, HotUpdateStatus.Failed, ex.Message, update.ToVersion, CancellationToken.None).ConfigureAwait(false);
            throw;
        }
    }

    public Task ReportInstallingAsync(HotUpdateInfo update, CancellationToken ct = default)
    {
        ArgumentNullException.ThrowIfNull(update);
        return ReportAsync(update.Id, update.FromVersion, HotUpdateStatus.Installing, toVersion: update.ToVersion, ct: ct);
    }

    public Task ReportSuccessAsync(HotUpdateInfo update, CancellationToken ct = default)
    {
        ArgumentNullException.ThrowIfNull(update);
        return ReportAsync(update.Id, update.FromVersion, HotUpdateStatus.Success, toVersion: update.ToVersion, ct: ct);
    }

    public Task ReportFailedAsync(HotUpdateInfo update, string errorMessage, CancellationToken ct = default)
    {
        ArgumentNullException.ThrowIfNull(update);
        return ReportAsync(update.Id, update.FromVersion, HotUpdateStatus.Failed, errorMessage, update.ToVersion, ct);
    }

    private async Task<long> DownloadFileAsync(string urlOrPath, string destinationPath, HotUpdateInfo update, IProgress<DownloadProgress>? progress, CancellationToken ct)
    {
        var fullPath = Path.GetFullPath(destinationPath);
        var directory = Path.GetDirectoryName(fullPath);
        if (!string.IsNullOrWhiteSpace(directory))
        {
            Directory.CreateDirectory(directory);
        }

        var tempPath = fullPath + ".part";
        try
        {
            using var resp = await _client.SendRawAsync(HttpMethod.Get, _client.BuildAbsoluteUrl(urlOrPath), content: null, authorize: false, allowRefresh: false, ct: ct).ConfigureAwait(false);
            var rawError = string.Empty;
            if (!resp.IsSuccessStatusCode)
            {
                rawError = await LicenseServerClient.SafeReadAsync(resp, ct).ConfigureAwait(false);
                throw new LicenseServerException(resp.StatusCode, LicenseServerClient.TryParseBusinessCode(rawError), LicenseServerClient.TryParseMessage(rawError) ?? $"download failed: HTTP {(int)resp.StatusCode}", rawError);
            }

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
                if (!string.IsNullOrWhiteSpace(update.FileHash) && !string.Equals(actualHash, update.FileHash, StringComparison.OrdinalIgnoreCase))
                {
                    throw new LicenseServerException("Downloaded hot update hash mismatch.", new InvalidDataException("SHA-256 mismatch."));
                }

                VerifyFileSignature(actualHash, written, update.FileSignature, update.SignatureAlg);
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

    private async Task<long> DownloadFileWithRefreshAsync(HotUpdateInfo update, string destinationPath, IProgress<DownloadProgress>? progress, CancellationToken ct)
    {
        try
        {
            return await DownloadFileAsync(update.DownloadUrl, destinationPath, update, progress, ct).ConfigureAwait(false);
        }
        catch (LicenseServerException ex) when (ex.IsUnauthorized || ex.HttpStatus == System.Net.HttpStatusCode.Forbidden)
        {
            if (string.IsNullOrWhiteSpace(update.FromVersion))
            {
                throw new LicenseServerException("Hot update download URL expired and update.FromVersion is empty; cannot refresh URL safely.", ex);
            }

            var refreshed = await CheckAsync(update.FromVersion, ct).ConfigureAwait(false);
            if (!refreshed.HasUpdate || refreshed.Id != update.Id || string.IsNullOrWhiteSpace(refreshed.DownloadUrl))
            {
                throw new LicenseServerException("Hot update download URL expired and no fresh URL is available.", ex);
            }

            CopyHotUpdateInfo(refreshed, update);
            return await DownloadFileAsync(update.DownloadUrl, destinationPath, update, progress, ct).ConfigureAwait(false);
        }
    }

    private static void CopyHotUpdateInfo(HotUpdateInfo source, HotUpdateInfo target)
    {
        target.HasUpdate = source.HasUpdate;
        target.Id = source.Id;
        target.FromVersion = source.FromVersion;
        target.ToVersion = source.ToVersion;
        target.PatchType = source.PatchType;
        target.UpdateType = source.UpdateType;
        target.DownloadUrl = source.DownloadUrl;
        target.FileSize = source.FileSize;
        target.FileHash = source.FileHash;
        target.FileSignature = source.FileSignature;
        target.SignatureAlg = source.SignatureAlg;
        target.Changelog = source.Changelog;
        target.ForceUpdate = source.ForceUpdate;
        target.MinAppVersion = source.MinAppVersion;
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
                throw new LicenseServerException("Downloaded hot update signature is missing.", new InvalidDataException("Signature missing."));
            }

            return;
        }

        if (!string.IsNullOrWhiteSpace(signatureAlg) && !string.Equals(signatureAlg, "RSA-SHA256", StringComparison.OrdinalIgnoreCase))
        {
            throw new LicenseServerException("Unsupported hot update signature algorithm: " + signatureAlg, new InvalidDataException("Unsupported signature algorithm."));
        }

        if (SignaturePublicKey is null)
        {
            if (RequireSignature)
            {
                throw new LicenseServerException("Hot update public key is not configured.", new InvalidOperationException("Missing public key."));
            }

            return;
        }

        try
        {
            var payload = Encoding.UTF8.GetBytes(fileHash.Trim().ToLowerInvariant() + ":" + fileSize);
            var signature = Convert.FromBase64String(fileSignature);
            if (!SignaturePublicKey.VerifyData(payload, signature, HashAlgorithmName.SHA256, RSASignaturePadding.Pkcs1))
            {
                throw new LicenseServerException("Downloaded hot update signature verification failed.", new InvalidDataException("Invalid signature."));
            }
        }
        catch (LicenseServerException)
        {
            throw;
        }
        catch (Exception ex)
        {
            throw new LicenseServerException("Downloaded hot update signature verification failed.", ex);
        }
    }

    private static T RequireData<T>(T? value, string message)
        where T : class
        => value ?? throw new LicenseServerException(message, new InvalidDataException("Response data is empty."));
}

public static class HotUpdateStatus
{
    public const string Pending = "pending";
    public const string Downloading = "downloading";
    public const string Installing = "installing";
    public const string Success = "success";
    public const string Failed = "failed";
    public const string Rollback = "rollback";
}

public sealed class HotUpdateInfo
{
    [JsonPropertyName("has_update")]
    public bool HasUpdate { get; set; }

    [JsonPropertyName("id")]
    public string Id { get; set; } = string.Empty;

    [JsonPropertyName("from_version")]
    public string FromVersion { get; set; } = string.Empty;

    [JsonPropertyName("to_version")]
    public string ToVersion { get; set; } = string.Empty;

    [JsonPropertyName("patch_type")]
    public string PatchType { get; set; } = string.Empty;

    [JsonPropertyName("update_type")]
    public string UpdateType { get; set; } = string.Empty;

    [JsonPropertyName("download_url")]
    public string DownloadUrl { get; set; } = string.Empty;

    [JsonPropertyName("file_size")]
    public long FileSize { get; set; }

    [JsonPropertyName("file_hash")]
    public string FileHash { get; set; } = string.Empty;

    [JsonPropertyName("file_signature")]
    public string FileSignature { get; set; } = string.Empty;

    [JsonPropertyName("signature_alg")]
    public string SignatureAlg { get; set; } = string.Empty;

    [JsonPropertyName("changelog")]
    public string Changelog { get; set; } = string.Empty;

    [JsonPropertyName("force_update")]
    public bool ForceUpdate { get; set; }

    [JsonPropertyName("min_app_version")]
    public string MinAppVersion { get; set; } = string.Empty;
}

public sealed class HotUpdateHistoryItem
{
    [JsonPropertyName("id")]
    public string Id { get; set; } = string.Empty;

    [JsonPropertyName("from_version")]
    public string FromVersion { get; set; } = string.Empty;

    [JsonPropertyName("to_version")]
    public string ToVersion { get; set; } = string.Empty;

    [JsonPropertyName("status")]
    public string Status { get; set; } = string.Empty;

    [JsonPropertyName("error_message")]
    public string ErrorMessage { get; set; } = string.Empty;

    [JsonPropertyName("changelog")]
    public string Changelog { get; set; } = string.Empty;

    [JsonPropertyName("started_at")]
    public DateTime? StartedAt { get; set; }

    [JsonPropertyName("completed_at")]
    public DateTime? CompletedAt { get; set; }
}

public readonly record struct DownloadProgress(long DownloadedBytes, long TotalBytes, double Ratio);
