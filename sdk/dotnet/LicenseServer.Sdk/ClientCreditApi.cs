using System.Text.Json.Serialization;

namespace LicenseServer.Sdk;

/// <summary>
/// Credit APIs for client-side SDK sessions under /api/client/credits.
/// </summary>
public sealed class ClientCreditApi
{
    private readonly LicenseClient _client;

    public ClientCreditApi(LicenseClient client)
    {
        _client = client;
    }

    public Task<MyCredit> GetMyCreditAsync(CancellationToken ct = default)
        => _client.GetAsync<MyCredit>("/credits/me", ct: ct);

    public Task<TransactionPage> GetMyTransactionsAsync(int page = 1, int pageSize = 50, CancellationToken ct = default)
        => _client.GetAsync<TransactionPage>("/credits/me/transactions", new Dictionary<string, string?>
        {
            ["page"] = page.ToString(),
            ["page_size"] = pageSize.ToString(),
        }, ct);

    public sealed class MyCredit
    {
        [JsonPropertyName("user_id")]
        public string UserId { get; set; } = string.Empty;

        [JsonPropertyName("balance")]
        public long Balance { get; set; }

        [JsonPropertyName("total_topup")]
        public long TotalTopup { get; set; }

        [JsonPropertyName("total_consumed")]
        public long TotalConsumed { get; set; }

        [JsonPropertyName("concurrent_limit")]
        public int ConcurrentLimit { get; set; }

        [JsonPropertyName("updated_at")]
        public DateTime? UpdatedAt { get; set; }
    }

    public sealed class TransactionPage
    {
        [JsonPropertyName("list")]
        public List<Transaction> List { get; set; } = [];

        [JsonPropertyName("total")]
        public long Total { get; set; }

        [JsonPropertyName("page")]
        public int Page { get; set; }

        [JsonPropertyName("page_size")]
        public int PageSize { get; set; }
    }

    public sealed class Transaction
    {
        [JsonPropertyName("id")]
        public long Id { get; set; }

        [JsonPropertyName("type")]
        public string Type { get; set; } = string.Empty;

        [JsonPropertyName("amount")]
        public long Amount { get; set; }

        [JsonPropertyName("balance_after")]
        public long BalanceAfter { get; set; }

        [JsonPropertyName("task_id")]
        public string TaskId { get; set; } = string.Empty;

        [JsonPropertyName("note")]
        public string Note { get; set; } = string.Empty;

        [JsonPropertyName("created_at")]
        public DateTime CreatedAt { get; set; }
    }
}
