using System.Collections.Concurrent;
using System.Text.Json;
using System.Text.Json.Serialization;

namespace LicenseServer.Sdk;

/// <summary>
/// Data sync endpoints under /api/client/sync.
/// </summary>
public sealed class DataSyncApi
{
    private readonly LicenseClient _client;
    private readonly ConcurrentDictionary<string, long> _lastSyncTime = new(StringComparer.Ordinal);

    internal DataSyncApi(LicenseClient client)
    {
        _client = client;
    }

    private static IReadOnlyDictionary<string, string?>? SyncQuery(IReadOnlyDictionary<string, string?>? extra = null)
        => extra;

    public async Task<List<TableInfo>> GetTableListAsync(CancellationToken ct = default)
        => await _client.GetAsync<List<TableInfo>>("/sync/tables", SyncQuery(), ct).ConfigureAwait(false) ?? [];

    public async Task<TablePullResult> PullTableAsync(string tableName, long since = 0, CancellationToken ct = default)
    {
        EnsureSyncIdentifier(tableName, nameof(tableName));
        var query = SyncQuery(new Dictionary<string, string?>
        {
            ["table"] = tableName,
            ["since"] = since > 0 ? since.ToString() : null,
        });

        var result = await _client.GetAsync<TablePullResult>("/sync/table", query, ct).ConfigureAwait(false) ?? new TablePullResult();
        result.Records ??= [];
        _lastSyncTime[tableName] = result.ServerTime;
        return result;
    }

    public async Task<AllTablesPullResult> PullAllTablesAsync(long since = 0, CancellationToken ct = default)
    {
        var result = await _client.GetAsync<AllTablesPullResult>("/sync/tables/all", SyncQuery(new Dictionary<string, string?>
        {
            ["since"] = since > 0 ? since.ToString() : null,
        }), ct).ConfigureAwait(false) ?? new AllTablesPullResult();
        result.Tables ??= [];
        foreach (var table in result.Tables.Keys.ToList())
        {
            result.Tables[table] ??= [];
        }
        return result;
    }

    public async Task<SyncResult> PushRecordAsync(string tableName, string recordId, IReadOnlyDictionary<string, object?> data, long version = 0, CancellationToken ct = default)
    {
        EnsureSyncIdentifier(tableName, nameof(tableName));
        EnsureSyncIdentifier(recordId, nameof(recordId));
        ArgumentNullException.ThrowIfNull(data);

        var result = await _client.PostAsync<SyncResult>("/sync/table", new
        {
            table = tableName,
            record_id = recordId,
            data,
            version,
        }, ct: ct).ConfigureAwait(false);
        result.RecordId = recordId;
        Normalize(result);
        return result;
    }

    public async Task<SyncTablePushBatchResult> PushRecordBatchAsync(string tableName, IEnumerable<PushRecordItem> records, CancellationToken ct = default)
    {
        EnsureSyncIdentifier(tableName, nameof(tableName));
        ArgumentNullException.ThrowIfNull(records);
        var items = records.ToList();
        foreach (var item in items)
        {
            EnsureSyncIdentifier(item.RecordId, nameof(item.RecordId));
        }

        var result = await _client.PostAsync<SyncTablePushBatchResult>("/sync/table/batch", new
        {
            table = tableName,
            records = items,
        }, ct: ct).ConfigureAwait(false) ?? new SyncTablePushBatchResult();
        result.Results ??= [];
        Normalize(result.Results);
        return result;
    }

    public async Task DeleteRecordAsync(string tableName, string recordId, CancellationToken ct = default)
    {
        EnsureSyncIdentifier(tableName, nameof(tableName));
        EnsureSyncIdentifier(recordId, nameof(recordId));

        await _client.SendNoDataAsync(HttpMethod.Delete, "/sync/table", new
        {
            table = tableName,
            record_id = recordId,
        }, ct: ct).ConfigureAwait(false);
    }

    public long GetLastSyncTime(string tableName)
    {
        EnsureSyncIdentifier(tableName, nameof(tableName));
        return _lastSyncTime.TryGetValue(tableName, out var value) ? value : 0;
    }

    public void SetLastSyncTime(string tableName, long value)
    {
        EnsureSyncIdentifier(tableName, nameof(tableName));
        _lastSyncTime[tableName] = value;
    }

    public async Task PushBackupAsync(string dataType, string dataJson, string? deviceName = null, int itemCount = 0, CancellationToken ct = default)
    {
        LicenseClient.EnsureNotEmpty(dataType, nameof(dataType));
        LicenseClient.EnsureNotEmpty(dataJson, nameof(dataJson));

        await _client.SendNoDataAsync(HttpMethod.Post, "/backup/push", new
        {
            data_type = dataType,
            data_json = dataJson,
            device_name = deviceName,
            item_count = itemCount,
        }, ct: ct).ConfigureAwait(false);
    }

    public async Task<List<BackupData>> PullBackupAsync(string dataType, CancellationToken ct = default)
    {
        LicenseClient.EnsureNotEmpty(dataType, nameof(dataType));
        var result = await _client.GetAsync<BackupPullResult>("/backup/pull", SyncQuery(new Dictionary<string, string?>
        {
            ["data_type"] = dataType,
        }), ct).ConfigureAwait(false) ?? new BackupPullResult();

        return result.Data ?? [];
    }

    public async Task<Dictionary<string, List<BackupData>>> PullAllBackupsAsync(CancellationToken ct = default)
    {
        var result = await _client.GetAsync<BackupPullResult>("/backup/pull", SyncQuery(), ct).ConfigureAwait(false) ?? new BackupPullResult();
        return (result.Data ?? [])
            .GroupBy(static x => x.DataType, StringComparer.Ordinal)
            .ToDictionary(static x => x.Key, static x => x.ToList(), StringComparer.Ordinal);
    }

    public async Task<SyncTableFromServerResult> SyncTableFromServerAsync(string tableName, long since = 0, CancellationToken ct = default)
    {
        var pulled = await PullTableAsync(tableName, since, ct).ConfigureAwait(false);
        var updates = new List<SyncRecord>();
        var deletes = new List<string>();
        foreach (var record in pulled.Records)
        {
            if (record.IsDeleted)
            {
                deletes.Add(record.Id);
            }
            else
            {
                updates.Add(record);
            }
        }

        return new SyncTableFromServerResult
        {
            Updates = updates,
            Deletes = deletes,
            ServerTime = pulled.ServerTime,
        };
    }

    public async Task<List<SyncResult>> SyncTableToServerAsync(string tableName, IEnumerable<IReadOnlyDictionary<string, object?>> records, string idField, CancellationToken ct = default)
    {
        EnsureSyncIdentifier(tableName, nameof(tableName));
        LicenseClient.EnsureNotEmpty(idField, nameof(idField));
        ArgumentNullException.ThrowIfNull(records);

        var items = new List<PushRecordItem>();
        foreach (var record in records)
        {
            if (!record.TryGetValue(idField, out var idValue) || idValue is null)
            {
                continue;
            }

            var recordId = Convert.ToString(idValue);
            if (string.IsNullOrWhiteSpace(recordId))
            {
                continue;
            }
            EnsureSyncIdentifier(recordId, nameof(recordId));

            items.Add(new PushRecordItem
            {
                RecordId = recordId,
                Data = new Dictionary<string, object?>(record, StringComparer.Ordinal),
            });
        }

        if (items.Count == 0)
        {
            return [];
        }

        var result = await PushRecordBatchAsync(tableName, items, ct).ConfigureAwait(false);
        return result.Results;
    }

    private static void EnsureSyncIdentifier(string value, string parameterName)
    {
        LicenseClient.EnsureNotEmpty(value, parameterName);
        if (value.Length > 100)
        {
            throw new ArgumentException(parameterName + " cannot exceed 100 characters.", parameterName);
        }
        foreach (var ch in value)
        {
            var allowed = ch is >= 'a' and <= 'z'
                or >= 'A' and <= 'Z'
                or >= '0' and <= '9'
                or '_'
                or '.'
                or '-';
            if (!allowed)
            {
                throw new ArgumentException(parameterName + " can only contain letters, digits, underscore, dot, and dash.", parameterName);
            }
        }
    }

    public async Task<SyncChangesPage> GetChangesAsync(long since = 0, IEnumerable<string>? dataTypes = null, int? limit = null, CancellationToken ct = default)
    {
        var types = dataTypes?.Where(static x => !string.IsNullOrWhiteSpace(x)).Select(static x => x.Trim()).Distinct(StringComparer.Ordinal).ToList() ?? [];
        if (types.Count > 1)
        {
            var merged = new SyncChangesPage();
            foreach (var type in types)
            {
                var page = await GetChangesByTypeAsync(since, type, limit, ct).ConfigureAwait(false);
                page.Changes ??= [];
                merged.Changes.AddRange(page.Changes);
                merged.ServerTime = Math.Max(merged.ServerTime, page.ServerTime);
                merged.HasMore = merged.HasMore || page.HasMore;
            }

            merged.Count = merged.Changes.Count;
            return merged;
        }

        return await GetChangesByTypeAsync(since, types.Count == 1 ? types[0] : null, limit, offset: null, autoPage: true, ct).ConfigureAwait(false);
    }

    public Task<SyncChangesPage> GetChangesPageAsync(long since = 0, string? dataType = null, int? limit = null, int? offset = null, CancellationToken ct = default)
    {
        return GetChangesByTypeAsync(since, dataType, limit, offset, autoPage: false, ct);
    }

    public async Task<SyncPushResult> PushChangesAsync(IEnumerable<SyncChange> changes, CancellationToken ct = default)
    {
        ArgumentNullException.ThrowIfNull(changes);
        var items = changes.Select(ToPushItem).ToList();
        var result = await _client.PostAsync<SyncPushResult>("/sync/push", new
        {
            items,
        }, ct: ct).ConfigureAwait(false) ?? new SyncPushResult();
        result.Results ??= [];
        Normalize(result.Results);
        return result;
    }

    public async Task<SyncStatusInfo> GetSyncStatusAsync(CancellationToken ct = default)
    {
        var result = await _client.GetAsync<SyncStatusInfo>("/sync/status", SyncQuery(), ct).ConfigureAwait(false) ?? new SyncStatusInfo();
        result.LastSync ??= [];
        return result;
    }

    public async Task<SyncResult> ResolveConflictAsync(string conflictId, string resolution, object? mergedData = null, CancellationToken ct = default)
    {
        LicenseClient.EnsureNotEmpty(conflictId, nameof(conflictId));
        LicenseClient.EnsureNotEmpty(resolution, nameof(resolution));

        await _client.SendNoDataAsync(HttpMethod.Post, "/sync/conflict/resolve", new
        {
            conflict_id = conflictId,
            resolution,
            merged_data = mergedData,
        }, ct: ct).ConfigureAwait(false);

        return new SyncResult
        {
            ConflictId = conflictId,
            Status = "resolved",
        };
    }

    public async Task<Dictionary<string, ConfigEntry>> GetConfigsAsync(CancellationToken ct = default)
        => await _client.GetAsync<Dictionary<string, ConfigEntry>>("/sync/configs", SyncQuery(), ct).ConfigureAwait(false) ?? [];

    public async Task<SyncResult> SaveConfigAsync(string configKey, object value, long version = 0, CancellationToken ct = default)
    {
        LicenseClient.EnsureNotEmpty(configKey, nameof(configKey));
        var result = await _client.PostAsync<SyncResult>("/sync/configs", new
        {
            config_key = configKey,
            value,
            version,
        }, ct: ct).ConfigureAwait(false);
        Normalize(result);
        return result;
    }

    public async Task<List<JsonElement>> GetWorkflowsAsync(CancellationToken ct = default)
        => await _client.GetAsync<List<JsonElement>>("/sync/workflows", SyncQuery(), ct).ConfigureAwait(false) ?? [];

    public async Task<SyncResult> SaveWorkflowAsync(object workflow, long version = 0, CancellationToken ct = default)
    {
        ArgumentNullException.ThrowIfNull(workflow);
        var result = await _client.PostAsync<SyncResult>("/sync/workflows", new
        {
            workflow,
            version,
        }, ct: ct).ConfigureAwait(false);
        Normalize(result);
        return result;
    }

    public async Task<SyncResult> DeleteWorkflowAsync(string workflowId, CancellationToken ct = default)
    {
        LicenseClient.EnsureNotEmpty(workflowId, nameof(workflowId));
        var result = await _client.DeleteAsync<SyncResult>("/sync/workflows/" + Uri.EscapeDataString(workflowId), query: SyncQuery(), ct: ct).ConfigureAwait(false) ?? new SyncResult();
        Normalize(result);
        return result;
    }

    public async Task<List<JsonElement>> GetMaterialsAsync(string? group = null, string? status = null, CancellationToken ct = default)
        => await _client.GetAsync<List<JsonElement>>("/sync/materials", SyncQuery(new Dictionary<string, string?>
        {
            ["group"] = group,
            ["status"] = status,
        }), ct).ConfigureAwait(false) ?? [];

    public async Task<SyncResult> SaveMaterialAsync(object material, long version = 0, CancellationToken ct = default)
    {
        ArgumentNullException.ThrowIfNull(material);
        var result = await _client.PostAsync<SyncResult>("/sync/materials", new
        {
            material,
            version,
        }, ct: ct).ConfigureAwait(false) ?? new SyncResult();
        Normalize(result);
        return result;
    }

    public async Task<SyncBatchResult> SaveMaterialsAsync(IEnumerable<object> materials, CancellationToken ct = default)
    {
        ArgumentNullException.ThrowIfNull(materials);
        var result = await _client.PostAsync<SyncBatchResult>("/sync/materials/batch", new
        {
            materials = materials.ToList(),
        }, ct: ct).ConfigureAwait(false) ?? new SyncBatchResult();
        Normalize(result);
        return result;
    }

    public async Task<PostListPage> GetPostsAsync(string? type = null, string? group = null, string? status = null, int page = 1, int pageSize = 100, CancellationToken ct = default)
    {
        var result = await _client.GetAsync<PostListPage>("/sync/posts", SyncQuery(new Dictionary<string, string?>
        {
            ["type"] = type,
            ["group"] = group,
            ["status"] = status,
            ["page"] = page.ToString(),
            ["page_size"] = pageSize.ToString(),
        }), ct).ConfigureAwait(false) ?? new PostListPage();
        result.List ??= [];
        return result;
    }

    public async Task<SyncBatchResult> SavePostsAsync(IEnumerable<object> posts, CancellationToken ct = default)
    {
        ArgumentNullException.ThrowIfNull(posts);
        var result = await _client.PostAsync<SyncBatchResult>("/sync/posts/batch", new
        {
            posts = posts.ToList(),
        }, ct: ct).ConfigureAwait(false) ?? new SyncBatchResult();
        Normalize(result);
        return result;
    }

    public async Task<PostStatusResult> UpdatePostStatusAsync(string postId, string status, CancellationToken ct = default)
    {
        LicenseClient.EnsureNotEmpty(postId, nameof(postId));
        LicenseClient.EnsureNotEmpty(status, nameof(status));
        return await _client.PutAsync<PostStatusResult>("/sync/posts/" + Uri.EscapeDataString(postId) + "/status", new
        {
            status,
        }, ct: ct).ConfigureAwait(false) ?? new PostStatusResult();
    }

    public async Task<List<PostGroupInfo>> GetPostGroupsAsync(string? type = null, CancellationToken ct = default)
        => await _client.GetAsync<List<PostGroupInfo>>("/sync/posts/groups", SyncQuery(new Dictionary<string, string?>
        {
            ["type"] = type,
        }), ct).ConfigureAwait(false) ?? [];

    public async Task<List<JsonElement>> GetCommentScriptsAsync(string? group = null, CancellationToken ct = default)
        => await _client.GetAsync<List<JsonElement>>("/sync/comment-scripts", SyncQuery(new Dictionary<string, string?>
        {
            ["group"] = group,
        }), ct).ConfigureAwait(false) ?? [];

    public async Task<SyncBatchResult> SaveCommentScriptsAsync(IEnumerable<object> scripts, CancellationToken ct = default)
    {
        ArgumentNullException.ThrowIfNull(scripts);
        var result = await _client.PostAsync<SyncBatchResult>("/sync/comment-scripts/batch", new
        {
            scripts = scripts.ToList(),
        }, ct: ct).ConfigureAwait(false) ?? new SyncBatchResult();
        Normalize(result);
        return result;
    }

    private async Task<SyncChangesPage> GetChangesByTypeAsync(long since, string? dataType, int? limit, int? offset, bool autoPage, CancellationToken ct)
    {
        var currentOffset = offset ?? 0;
        var merged = new SyncChangesPage();
        while (true)
        {
            var page = await _client.GetAsync<SyncChangesPage>("/sync/changes", SyncQuery(new Dictionary<string, string?>
            {
                ["since"] = since > 0 ? since.ToString() : null,
                ["data_type"] = dataType,
                ["limit"] = limit is > 0 ? limit.Value.ToString() : null,
                ["offset"] = currentOffset > 0 ? currentOffset.ToString() : null,
            }), ct).ConfigureAwait(false) ?? new SyncChangesPage();

            page.Changes ??= [];
            merged.Changes.AddRange(page.Changes);
            merged.ServerTime = Math.Max(merged.ServerTime, page.ServerTime);
            merged.Limit = page.Limit;
            merged.Offset = offset ?? 0;
            merged.NextOffset = page.NextOffset;
            merged.HasMore = page.HasMore;

            if (!autoPage || !page.HasMore)
            {
                merged.Count = merged.Changes.Count;
                return merged;
            }
            if (page.NextOffset <= currentOffset)
            {
                throw new LicenseServerException("Server returned invalid next_offset.", new InvalidOperationException("Invalid sync pagination state."));
            }
            currentOffset = page.NextOffset;
        }
    }

    private Task<SyncChangesPage> GetChangesByTypeAsync(long since, string? dataType, int? limit, CancellationToken ct)
        => GetChangesByTypeAsync(since, dataType, limit, offset: null, autoPage: true, ct);

    private static PushChangeItem ToPushItem(SyncChange change)
    {
        var dataType = FirstNonEmpty(change.DataType, change.Table).Trim();
        var dataKey = FirstNonEmpty(change.DataKey, change.RecordId, change.Id).Trim();
        var action = FirstNonEmpty(change.Action, NormalizeOperation(change.Operation), "update").Trim().ToLowerInvariant();
        var localVersion = change.LocalVersion != 0 ? change.LocalVersion : change.Version;

        if (string.IsNullOrWhiteSpace(dataType))
        {
            throw new ArgumentException("Sync change data_type cannot be empty.", nameof(change));
        }
        if (string.IsNullOrWhiteSpace(dataKey))
        {
            throw new ArgumentException("Sync change data_key cannot be empty.", nameof(change));
        }
        if (!IsValidAction(action))
        {
            throw new ArgumentException("Sync change action must be create, update, or delete.", nameof(change));
        }

        return new PushChangeItem
        {
            DataType = dataType,
            DataKey = dataKey,
            Action = action,
            Data = change.Data,
            LocalVersion = localVersion,
        };
    }

    private static string NormalizeOperation(string value)
        => value.Trim().ToLowerInvariant() switch
        {
            "insert" => "create",
            "create" => "create",
            "delete" => "delete",
            _ => "update",
        };

    private static bool IsValidAction(string value)
        => value is "create" or "update" or "delete";

    private static string FirstNonEmpty(params string?[] values)
        => values.FirstOrDefault(static x => !string.IsNullOrWhiteSpace(x)) ?? string.Empty;

    private static void Normalize(IEnumerable<SyncResult> results)
    {
        foreach (var result in results)
        {
            if (result is not null)
            {
                Normalize(result);
            }
        }
    }

    private static void Normalize(SyncResult result)
    {
        if (string.IsNullOrWhiteSpace(result.RecordId))
        {
            result.RecordId = result.DataKey;
        }
        if (string.IsNullOrWhiteSpace(result.DataKey))
        {
            result.DataKey = result.RecordId;
        }
        if (result.Version == 0)
        {
            result.Version = result.ServerVersion;
        }
    }

    private static void Normalize(SyncBatchResult result)
    {
        result.Results ??= [];
        Normalize(result.Results);
    }
}

public static class DataSyncTypes
{
    public const string Config = "config";
    public const string Workflow = "workflow";
    public const string BatchTask = "batch_task";
    public const string Material = "material";
    public const string Post = "post";
    public const string Comment = "comment";
    public const string CommentScript = "comment_script";
    public const string VoiceConfig = "voice_config";
    public const string Scripts = "scripts";
    public const string DanmakuGroups = "danmaku_groups";
    public const string AIConfig = "ai_config";
    public const string RandomWordAIConfig = "random_word_ai_config";
}

public sealed class BackupPullResult
{
    [JsonPropertyName("data")]
    public List<BackupData> Data { get; set; } = [];
}

public sealed class BackupData
{
    [JsonPropertyName("id")]
    public string Id { get; set; } = string.Empty;

    [JsonPropertyName("data_type")]
    public string DataType { get; set; } = string.Empty;

    [JsonPropertyName("data_json")]
    public string DataJson { get; set; } = string.Empty;

    [JsonPropertyName("version")]
    public int Version { get; set; }

    [JsonPropertyName("device_name")]
    public string DeviceName { get; set; } = string.Empty;

    [JsonPropertyName("machine_id")]
    public string MachineId { get; set; } = string.Empty;

    [JsonPropertyName("is_current")]
    public bool IsCurrent { get; set; }

    [JsonPropertyName("updated_at")]
    public string UpdatedAt { get; set; } = string.Empty;
}

public static class ConflictResolution
{
    public const string UseLocal = "use_local";
    public const string UseServer = "use_server";
    public const string Merge = "merge";
}

public sealed class SyncRecord
{
    [JsonPropertyName("id")]
    public string Id { get; set; } = string.Empty;

    [JsonPropertyName("data")]
    public Dictionary<string, object?> Data { get; set; } = [];

    [JsonPropertyName("version")]
    public long Version { get; set; }

    [JsonPropertyName("is_deleted")]
    public bool IsDeleted { get; set; }

    [JsonPropertyName("updated_at")]
    public long UpdatedAt { get; set; }
}

public sealed class SyncResult
{
    [JsonPropertyName("record_id")]
    public string RecordId { get; set; } = string.Empty;

    [JsonPropertyName("data_key")]
    public string DataKey { get; set; } = string.Empty;

    [JsonPropertyName("status")]
    public string Status { get; set; } = string.Empty;

    [JsonPropertyName("version")]
    public long Version { get; set; }

    [JsonPropertyName("server_version")]
    public long ServerVersion { get; set; }

    [JsonPropertyName("conflict_id")]
    public string ConflictId { get; set; } = string.Empty;

    [JsonPropertyName("error")]
    public string Error { get; set; } = string.Empty;

    [JsonPropertyName("conflict_data")]
    public object? ConflictData { get; set; }
}

public sealed class TableInfo
{
    [JsonPropertyName("table_name")]
    public string TableName { get; set; } = string.Empty;

    [JsonPropertyName("record_count")]
    public long RecordCount { get; set; }

    [JsonPropertyName("last_updated")]
    public string LastUpdated { get; set; } = string.Empty;
}

public sealed class TablePullResult
{
    [JsonPropertyName("table")]
    public string Table { get; set; } = string.Empty;

    [JsonPropertyName("records")]
    public List<SyncRecord> Records { get; set; } = [];

    [JsonPropertyName("count")]
    public int Count { get; set; }

    [JsonPropertyName("server_time")]
    public long ServerTime { get; set; }
}

public sealed class AllTablesPullResult
{
    [JsonPropertyName("tables")]
    public Dictionary<string, List<SyncRecord>> Tables { get; set; } = [];

    [JsonPropertyName("server_time")]
    public long ServerTime { get; set; }
}

public sealed class PushRecordItem
{
    [JsonPropertyName("record_id")]
    public string RecordId { get; set; } = string.Empty;

    [JsonPropertyName("data")]
    public Dictionary<string, object?> Data { get; set; } = [];

    [JsonPropertyName("version")]
    public long Version { get; set; }

    [JsonPropertyName("deleted")]
    public bool Deleted { get; set; }
}

public sealed class SyncTablePushBatchResult
{
    [JsonPropertyName("table")]
    public string Table { get; set; } = string.Empty;

    [JsonPropertyName("results")]
    public List<SyncResult> Results { get; set; } = [];

    [JsonPropertyName("count")]
    public int Count { get; set; }

    [JsonPropertyName("server_time")]
    public long ServerTime { get; set; }
}

public sealed class SyncTableFromServerResult
{
    public List<SyncRecord> Updates { get; set; } = [];

    public List<string> Deletes { get; set; } = [];

    public long ServerTime { get; set; }
}

public sealed class SyncChange
{
    [JsonPropertyName("id")]
    public string Id { get; set; } = string.Empty;

    [JsonPropertyName("data_type")]
    public string DataType { get; set; } = string.Empty;

    [JsonPropertyName("data_key")]
    public string DataKey { get; set; } = string.Empty;

    [JsonPropertyName("action")]
    public string Action { get; set; } = string.Empty;

    [JsonPropertyName("table")]
    public string Table { get; set; } = string.Empty;

    [JsonPropertyName("record_id")]
    public string RecordId { get; set; } = string.Empty;

    [JsonPropertyName("operation")]
    public string Operation { get; set; } = string.Empty;

    [JsonPropertyName("data")]
    public object? Data { get; set; }

    [JsonPropertyName("version")]
    public long Version { get; set; }

    [JsonPropertyName("local_version")]
    public long LocalVersion { get; set; }

    [JsonPropertyName("updated_at")]
    public long UpdatedAt { get; set; }

    [JsonPropertyName("change_time")]
    public long ChangeTime { get; set; }
}

public sealed class SyncChangesPage
{
    [JsonPropertyName("changes")]
    public List<SyncChange> Changes { get; set; } = [];

    [JsonPropertyName("count")]
    public int Count { get; set; }

    [JsonPropertyName("has_more")]
    public bool HasMore { get; set; }

    [JsonPropertyName("next_offset")]
    public int NextOffset { get; set; }

    [JsonPropertyName("limit")]
    public int Limit { get; set; }

    [JsonPropertyName("offset")]
    public int Offset { get; set; }

    [JsonPropertyName("server_time")]
    public long ServerTime { get; set; }
}

public sealed class SyncPushResult
{
    [JsonPropertyName("results")]
    public List<SyncResult> Results { get; set; } = [];

    [JsonPropertyName("success_count")]
    public int SuccessCount { get; set; }

    [JsonPropertyName("conflict_count")]
    public int ConflictCount { get; set; }

    [JsonPropertyName("error_count")]
    public int ErrorCount { get; set; }

    [JsonPropertyName("server_time")]
    public long ServerTime { get; set; }
}

public sealed class SyncStatusInfo
{
    [JsonPropertyName("stats")]
    public JsonElement Stats { get; set; }

    [JsonPropertyName("last_sync")]
    public Dictionary<string, long> LastSync { get; set; } = [];

    [JsonPropertyName("server_time")]
    public long ServerTime { get; set; }
}

public sealed class ConfigEntry
{
    [JsonPropertyName("value")]
    public JsonElement Value { get; set; }

    [JsonPropertyName("version")]
    public long Version { get; set; }

    [JsonPropertyName("updated_at")]
    public long UpdatedAt { get; set; }
}

public sealed class SyncBatchResult
{
    [JsonPropertyName("results")]
    public List<SyncResult> Results { get; set; } = [];

    [JsonPropertyName("count")]
    public int Count { get; set; }
}

public sealed class PostListPage
{
    [JsonPropertyName("list")]
    public List<JsonElement> List { get; set; } = [];

    [JsonPropertyName("total")]
    public long Total { get; set; }

    [JsonPropertyName("page")]
    public int Page { get; set; }

    [JsonPropertyName("page_size")]
    public int PageSize { get; set; }
}

public sealed class PostStatusResult
{
    [JsonPropertyName("version")]
    public long Version { get; set; }
}

public sealed class PostGroupInfo
{
    [JsonPropertyName("group_name")]
    public string GroupName { get; set; } = string.Empty;

    [JsonPropertyName("post_type")]
    public string PostType { get; set; } = string.Empty;

    [JsonPropertyName("total_count")]
    public long TotalCount { get; set; }

    [JsonPropertyName("unused_count")]
    public long UnusedCount { get; set; }

    [JsonPropertyName("used_count")]
    public long UsedCount { get; set; }
}

internal sealed class PushChangeItem
{
    [JsonPropertyName("data_type")]
    public string DataType { get; set; } = string.Empty;

    [JsonPropertyName("data_key")]
    public string DataKey { get; set; } = string.Empty;

    [JsonPropertyName("action")]
    public string Action { get; set; } = string.Empty;

    [JsonPropertyName("data")]
    public object? Data { get; set; }

    [JsonPropertyName("local_version")]
    public long LocalVersion { get; set; }
}

