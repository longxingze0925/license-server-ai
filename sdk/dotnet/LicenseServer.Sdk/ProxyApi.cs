using System.Net.Http.Headers;
using System.Text;
using System.Text.Json;
using System.Text.Json.Serialization;

namespace LicenseServer.Sdk;

/// <summary>
/// SDK wrapper for license-server /api/proxy/* endpoints.
/// </summary>
public sealed class ProxyApi
{
    private readonly LicenseServerClient? _serverClient;
    private readonly LicenseClient? _client;
    private readonly string _basePath;

    public ProxyApi(LicenseServerClient client)
    {
        _serverClient = client;
        _basePath = "/api/proxy";
    }

    public ProxyApi(LicenseClient client)
    {
        _client = client;
        _basePath = "/proxy";
    }

    public async Task<CapabilityCatalog> GetCapabilitiesAsync(CancellationToken ct = default)
    {
        var result = await SendProxyJsonAsync(HttpMethod.Get, "/capabilities", body: null, static () => new CapabilityCatalog(), ct).ConfigureAwait(false);
        Normalize(result);
        return result;
    }

    public Task<ChatResult> ChatAsync(string providerSlug, object body, CancellationToken ct = default)
        => ChatAsync(providerSlug, body, mode: null, scope: null, channelId: null, ct);

    public Task<ChatResult> ChatAsync(string providerSlug, object body, string? mode, CancellationToken ct = default)
        => ChatAsync(providerSlug, body, mode, scope: null, channelId: null, ct);

    public Task<ChatResult> ChatAsync(string providerSlug, object body, string? mode, string? scope, CancellationToken ct = default)
        => ChatAsync(providerSlug, body, mode, scope, channelId: null, ct);

    public async Task<ChatResult> ChatAsync(string providerSlug, object body, string? mode, string? scope, string? channelId, CancellationToken ct = default)
    {
        var query = BuildProxyQuery(mode, scope, channelId);
        var content = System.Net.Http.Json.JsonContent.Create(body, options: LicenseServerClient.JsonOptions);
        using var resp = await SendProxyRawAsync(HttpMethod.Post, $"/{providerSlug}/chat{query}", content, ct).ConfigureAwait(false);
        var raw = await resp.Content.ReadAsStringAsync(ct).ConfigureAwait(false);
        if (!resp.IsSuccessStatusCode)
        {
            throw new LicenseServerException(resp.StatusCode, LicenseServerClient.TryParseBusinessCode(raw), LicenseServerClient.TryParseMessage(raw) ?? $"chat failed: HTTP {(int)resp.StatusCode}", raw);
        }

        return new ChatResult
        {
            TaskId = resp.Headers.TryGetValues("X-Task-Id", out var ids) ? ids.FirstOrDefault() ?? string.Empty : string.Empty,
            Cost = resp.Headers.TryGetValues("X-Cost", out var costs) && int.TryParse(costs.FirstOrDefault(), out var cost) ? cost : 0,
            Body = raw,
        };
    }

    public Task<GenerateResult> GenerateAsync(string providerSlug, object body, string? mode = null, string? scope = null, CancellationToken ct = default)
        => GenerateAsync(providerSlug, body, mode, scope, channelId: null, ct);

    public async Task<GenerateResult> GenerateAsync(string providerSlug, object body, string? mode, string? scope, string? channelId, CancellationToken ct = default)
    {
        var query = BuildProxyQuery(mode, scope, channelId, clientModel: null);
        var result = await SendProxyJsonAsync(HttpMethod.Post, $"/{providerSlug}/generate{query}", body, static () => new GenerateResult(), ct).ConfigureAwait(false);
        Normalize(result);
        return result;
    }

    public async Task<GenerateResult> GenerateAsync(string providerSlug, object body, string? mode, string? scope, string? channelId, string? clientModel, CancellationToken ct = default)
    {
        var query = BuildProxyQuery(mode, scope, channelId, clientModel);
        var result = await SendProxyJsonAsync(HttpMethod.Post, $"/{providerSlug}/generate{query}", body, static () => new GenerateResult(), ct).ConfigureAwait(false);
        Normalize(result);
        return result;
    }

    public Task<GenerateResult> GenerateAsync(string providerSlug, object body, IReadOnlyList<GenerateUploadFile> uploads, string? mode = null, string? scope = null, CancellationToken ct = default)
        => GenerateAsync(providerSlug, body, uploads, mode, scope, channelId: null, ct);

    public async Task<GenerateResult> GenerateAsync(string providerSlug, object body, IReadOnlyList<GenerateUploadFile> uploads, string? mode, string? scope, string? channelId, CancellationToken ct = default)
        => await GenerateAsync(providerSlug, body, uploads, mode, scope, channelId, clientModel: null, ct).ConfigureAwait(false);

    public async Task<GenerateResult> GenerateAsync(string providerSlug, object body, IReadOnlyList<GenerateUploadFile> uploads, string? mode, string? scope, string? channelId, string? clientModel, CancellationToken ct = default)
    {
        if (uploads.Count == 0)
        {
            return await GenerateAsync(providerSlug, body, mode, scope, channelId, clientModel, ct).ConfigureAwait(false);
        }

        var query = BuildProxyQuery(mode, scope, channelId, clientModel);
        using var content = new MultipartFormDataContent();
        var payload = JsonSerializer.Serialize(body, LicenseServerClient.JsonOptions);
        content.Add(new StringContent(payload, Encoding.UTF8, "application/json"), "payload");

        foreach (var upload in uploads)
        {
            var stream = new FileStream(upload.FilePath, FileMode.Open, FileAccess.Read, FileShare.Read, 81920, useAsync: true);
            var streamContent = new StreamContent(stream);
            if (!string.IsNullOrWhiteSpace(upload.MimeType))
            {
                streamContent.Headers.ContentType = new MediaTypeHeaderValue(upload.MimeType);
            }

            var fileName = string.IsNullOrWhiteSpace(upload.FileName) ? Path.GetFileName(upload.FilePath) : upload.FileName;
            content.Add(streamContent, "images", fileName);
        }

        using var resp = await SendProxyRawAsync(HttpMethod.Post, $"/{providerSlug}/generate{query}", content, ct).ConfigureAwait(false);
        var raw = await resp.Content.ReadAsStringAsync(ct).ConfigureAwait(false);
        if (!resp.IsSuccessStatusCode)
        {
            throw new LicenseServerException(resp.StatusCode, LicenseServerClient.TryParseBusinessCode(raw), LicenseServerClient.TryParseMessage(raw) ?? $"generate failed: HTTP {(int)resp.StatusCode}", raw);
        }

        var parsed = JsonSerializer.Deserialize<ApiResponse<GenerateResult>>(raw, LicenseServerClient.JsonOptions)
            ?? throw new LicenseServerException(resp.StatusCode, 0, "Response cannot be parsed.", raw);
        if (parsed.Code != 0)
        {
            throw new LicenseServerException(resp.StatusCode, parsed.Code, parsed.Message ?? "Unknown server error.", raw);
        }

        var result = parsed.Data ?? new GenerateResult();
        Normalize(result);
        return result;
    }

    public async Task<TaskDetail> GetTaskAsync(string taskId, CancellationToken ct = default)
    {
        var result = await SendProxyJsonAsync(HttpMethod.Get, $"/tasks/{Uri.EscapeDataString(taskId)}", body: null, static () => new TaskDetail(), ct).ConfigureAwait(false);
        Normalize(result);
        return result;
    }

    public async Task<TaskListPage> ListMyTasksAsync(int page = 1, int pageSize = 50, CancellationToken ct = default)
    {
        var result = await SendProxyJsonAsync(HttpMethod.Get, $"/tasks?page={page}&page_size={pageSize}", body: null, static () => new TaskListPage(), ct).ConfigureAwait(false);
        Normalize(result);
        return result;
    }

    public async Task<FileListPage> ListMyFilesAsync(int page = 1, int pageSize = 50, CancellationToken ct = default)
    {
        var result = await SendProxyJsonAsync(HttpMethod.Get, $"/files?page={page}&page_size={pageSize}", body: null, static () => new FileListPage(), ct).ConfigureAwait(false);
        Normalize(result);
        return result;
    }

    public Task<object> DeleteFileAsync(string fileId, CancellationToken ct = default)
        => SendProxyJsonAsync(HttpMethod.Delete, $"/files/{Uri.EscapeDataString(fileId)}", body: null, static () => new object(), ct);

    public async Task<long> DownloadFileAsync(string fileId, string destinationPath, CancellationToken ct = default)
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
            using var resp = await SendProxyRawAsync(HttpMethod.Get, $"/files/{Uri.EscapeDataString(fileId)}", content: null, ct).ConfigureAwait(false);
            if (!resp.IsSuccessStatusCode)
            {
                var raw = await resp.Content.ReadAsStringAsync(ct).ConfigureAwait(false);
                throw new LicenseServerException(resp.StatusCode, LicenseServerClient.TryParseBusinessCode(raw), LicenseServerClient.TryParseMessage(raw) ?? $"download failed: HTTP {(int)resp.StatusCode}", raw);
            }

            long written;
            await using (var input = await resp.Content.ReadAsStreamAsync(ct).ConfigureAwait(false))
            await using (var output = new FileStream(tempPath, FileMode.Create, FileAccess.Write, FileShare.None, 81920, useAsync: true))
            {
                await input.CopyToAsync(output, ct).ConfigureAwait(false);
                written = output.Length;
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

    private async Task<T> SendProxyJsonAsync<T>(HttpMethod method, string path, object? body, Func<T> emptyFactory, CancellationToken ct)
    {
        using var content = body is null ? null : System.Net.Http.Json.JsonContent.Create(body, options: LicenseServerClient.JsonOptions);
        using var resp = await SendProxyRawAsync(method, path, content, ct).ConfigureAwait(false);
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

        return parsed.Data ?? emptyFactory();
    }

    private Task<HttpResponseMessage> SendProxyRawAsync(HttpMethod method, string path, HttpContent? content, CancellationToken ct)
    {
        var proxyPath = BuildProxyPath(path);
        if (_serverClient is not null)
        {
            return _serverClient.SendRawAsync(method, proxyPath, content, ct);
        }

        if (_client is null)
        {
            throw new InvalidOperationException("ProxyApi has no HTTP client.");
        }

        return _client.SendRawAsync(method, proxyPath, content, ct: ct);
    }

    private string BuildProxyPath(string path)
    {
        if (!path.StartsWith('/'))
        {
            path = "/" + path;
        }

        return _basePath + path;
    }

    private static void Normalize(CapabilityCatalog catalog)
    {
        catalog.Providers ??= [];
        foreach (var provider in catalog.Providers)
        {
            provider.Channels ??= [];
            foreach (var channel in provider.Channels)
            {
                channel.SupportedModes ??= [];
                channel.SupportedScopes ??= [];
                channel.SupportedAspectRatios ??= [];
                channel.SupportedDurations ??= [];
                channel.Models ??= [];
                foreach (var model in channel.Models)
                {
                    model.SupportedModes ??= [];
                    model.SupportedScopes ??= [];
                }
            }
        }
    }

    private static void Normalize(GenerateResult result)
    {
        result.TaskId ??= string.Empty;
        result.Status ??= string.Empty;
    }

    private static void Normalize(TaskDetail detail)
    {
        detail.Task ??= new TaskInfo();
        detail.Files ??= [];
    }

    private static void Normalize(TaskListPage page)
    {
        page.List ??= [];
    }

    private static void Normalize(FileListPage page)
    {
        page.List ??= [];
    }

    private static string BuildProxyQuery(string? mode, string? scope, string? channelId)
        => BuildProxyQuery(mode, scope, channelId, clientModel: null);

    private static string BuildProxyQuery(string? mode, string? scope, string? channelId, string? clientModel)
    {
        var parts = new List<string>(capacity: 4);
        if (!string.IsNullOrWhiteSpace(mode))
        {
            parts.Add("mode=" + Uri.EscapeDataString(mode));
        }
        if (!string.IsNullOrWhiteSpace(scope))
        {
            parts.Add("scope=" + Uri.EscapeDataString(scope));
        }
        if (!string.IsNullOrWhiteSpace(channelId))
        {
            parts.Add("channel_id=" + Uri.EscapeDataString(channelId));
        }
        if (!string.IsNullOrWhiteSpace(clientModel))
        {
            parts.Add("client_model=" + Uri.EscapeDataString(clientModel));
        }

        return parts.Count == 0 ? string.Empty : "?" + string.Join("&", parts);
    }

    public sealed class CapabilityCatalog
    {
        [JsonPropertyName("providers")]
        public List<ProviderCapabilityInfo> Providers { get; set; } = [];
    }

    public sealed class ProviderCapabilityInfo
    {
        [JsonPropertyName("provider")]
        public string Provider { get; set; } = string.Empty;

        [JsonPropertyName("display_name")]
        public string DisplayName { get; set; } = string.Empty;

        [JsonPropertyName("description")]
        public string Description { get; set; } = string.Empty;

        [JsonPropertyName("default_base_url")]
        public string DefaultBaseUrl { get; set; } = string.Empty;

        [JsonPropertyName("default_model")]
        public string DefaultModel { get; set; } = string.Empty;

        [JsonPropertyName("channels")]
        public List<ProviderChannelInfo> Channels { get; set; } = [];
    }

    public sealed class ProviderChannelInfo
    {
        [JsonPropertyName("channel_id")]
        public string ChannelId { get; set; } = string.Empty;

        [JsonPropertyName("channel_name")]
        public string ChannelName { get; set; } = string.Empty;

        [JsonPropertyName("mode")]
        public string Mode { get; set; } = string.Empty;

        [JsonPropertyName("base_url")]
        public string BaseUrl { get; set; } = string.Empty;

        [JsonPropertyName("default_model")]
        public string DefaultModel { get; set; } = string.Empty;

        [JsonPropertyName("priority")]
        public int Priority { get; set; }

        [JsonPropertyName("sort_order")]
        public int SortOrder { get; set; }

        [JsonPropertyName("is_default")]
        public bool IsDefault { get; set; }

        [JsonPropertyName("enabled")]
        public bool Enabled { get; set; } = true;

        [JsonPropertyName("health_status")]
        public string HealthStatus { get; set; } = string.Empty;

        [JsonPropertyName("supported_modes")]
        public List<string> SupportedModes { get; set; } = [];

        [JsonPropertyName("supported_scopes")]
        public List<string> SupportedScopes { get; set; } = [];

        [JsonPropertyName("supported_aspect_ratios")]
        public List<string> SupportedAspectRatios { get; set; } = [];

        [JsonPropertyName("supported_durations")]
        public List<string> SupportedDurations { get; set; } = [];

        [JsonPropertyName("supported_resolutions")]
        public List<string> SupportedResolutions { get; set; } = [];

        [JsonPropertyName("models")]
        public List<ProviderModelInfo> Models { get; set; } = [];
    }

    public sealed class ProviderModelInfo
    {
        [JsonPropertyName("id")]
        public string Id { get; set; } = string.Empty;

        [JsonPropertyName("display_name")]
        public string DisplayName { get; set; } = string.Empty;

        [JsonPropertyName("supported_modes")]
        public List<string> SupportedModes { get; set; } = [];

        [JsonPropertyName("supported_scopes")]
        public List<string> SupportedScopes { get; set; } = [];
    }

    public sealed class ChatResult
    {
        public string TaskId { get; set; } = string.Empty;

        public int Cost { get; set; }

        public string Body { get; set; } = string.Empty;
    }

    public sealed class GenerateResult
    {
        [JsonPropertyName("task_id")]
        public string TaskId { get; set; } = string.Empty;

        [JsonPropertyName("cost")]
        public int Cost { get; set; }

        [JsonPropertyName("status")]
        public string Status { get; set; } = string.Empty;
    }

    public sealed class GenerateUploadFile
    {
        public string FilePath { get; set; } = string.Empty;

        public string FileName { get; set; } = string.Empty;

        public string MimeType { get; set; } = string.Empty;
    }

    public sealed class TaskDetail
    {
        [JsonPropertyName("task")]
        public TaskInfo Task { get; set; } = new();

        [JsonPropertyName("files")]
        public List<FileInfo> Files { get; set; } = [];
    }

    public sealed class TaskInfo
    {
        [JsonPropertyName("id")]
        public string Id { get; set; } = string.Empty;

        [JsonPropertyName("provider")]
        public string Provider { get; set; } = string.Empty;

        [JsonPropertyName("mode")]
        public string Mode { get; set; } = string.Empty;

        [JsonPropertyName("status")]
        public int Status { get; set; }

        [JsonPropertyName("progress")]
        public double Progress { get; set; }

        [JsonPropertyName("cost")]
        public int Cost { get; set; }

        [JsonPropertyName("upstream_task_id")]
        public string UpstreamTaskId { get; set; } = string.Empty;

        [JsonPropertyName("error_json")]
        public string? ErrorJson { get; set; }

        [JsonPropertyName("result_json")]
        public string? ResultJson { get; set; }

        [JsonPropertyName("upstream_status")]
        public string UpstreamStatus { get; set; } = string.Empty;

        [JsonPropertyName("upstream_error")]
        public string UpstreamError { get; set; } = string.Empty;

        [JsonPropertyName("refund_status")]
        public string RefundStatus { get; set; } = string.Empty;

        [JsonPropertyName("refund_amount")]
        public long RefundAmount { get; set; }

        [JsonPropertyName("refunded_at")]
        public DateTime? RefundedAt { get; set; }

        [JsonPropertyName("created_at")]
        public DateTime CreatedAt { get; set; }

        [JsonPropertyName("completed_at")]
        public DateTime? CompletedAt { get; set; }

        public string ParseErrorMessage()
        {
            if (string.IsNullOrWhiteSpace(ErrorJson))
            {
                return string.Empty;
            }

            try
            {
                using var doc = JsonDocument.Parse(ErrorJson);
                if (doc.RootElement.TryGetProperty("reason", out var reason))
                {
                    return reason.GetString() ?? ErrorJson;
                }
            }
            catch
            {
            }

            return ErrorJson;
        }
    }

    public sealed class FileInfo
    {
        [JsonPropertyName("id")]
        public string Id { get; set; } = string.Empty;

        [JsonPropertyName("task_id")]
        public string TaskId { get; set; } = string.Empty;

        [JsonPropertyName("kind")]
        public string Kind { get; set; } = string.Empty;

        [JsonPropertyName("size_bytes")]
        public long SizeBytes { get; set; }

        [JsonPropertyName("mime_type")]
        public string MimeType { get; set; } = string.Empty;

        [JsonPropertyName("duration_ms")]
        public int DurationMs { get; set; }

        [JsonPropertyName("width")]
        public int Width { get; set; }

        [JsonPropertyName("height")]
        public int Height { get; set; }

        [JsonPropertyName("path")]
        public string Path { get; set; } = string.Empty;

        [JsonPropertyName("expires_at")]
        public DateTime ExpiresAt { get; set; }
    }

    public sealed class TaskListPage
    {
        [JsonPropertyName("list")]
        public List<TaskInfo> List { get; set; } = [];

        [JsonPropertyName("total")]
        public long Total { get; set; }

        [JsonPropertyName("page")]
        public int Page { get; set; }

        [JsonPropertyName("page_size")]
        public int PageSize { get; set; }
    }

    public sealed class FileListPage
    {
        [JsonPropertyName("list")]
        public List<FileInfo> List { get; set; } = [];

        [JsonPropertyName("total")]
        public long Total { get; set; }

        [JsonPropertyName("page")]
        public int Page { get; set; }

        [JsonPropertyName("page_size")]
        public int PageSize { get; set; }
    }
}
