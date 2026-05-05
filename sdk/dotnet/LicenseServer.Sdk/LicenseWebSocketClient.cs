using System.Net.WebSockets;
using System.Security.Cryptography;
using System.Text;
using System.Text.Json;
using System.Text.Json.Serialization;

namespace LicenseServer.Sdk;

/// <summary>
/// WebSocket client for realtime instructions under /api/client/ws.
/// </summary>
public sealed class LicenseWebSocketClient : IAsyncDisposable
{
    private static readonly TimeSpan AuthTimeout = TimeSpan.FromSeconds(10);
    private static readonly TimeSpan PingInterval = TimeSpan.FromSeconds(30);
    private static readonly TimeSpan TokenRefreshSkew = TimeSpan.FromSeconds(60);

    private readonly LicenseClient _client;
    private readonly Dictionary<string, Func<RealtimeInstruction, CancellationToken, Task<object?>>> _handlers = new(StringComparer.OrdinalIgnoreCase);
    private readonly SemaphoreSlim _sendLock = new(1, 1);
    private readonly SemaphoreSlim _stateLock = new(1, 1);
    private readonly object _reconnectLock = new();
    private ClientWebSocket? _socket;
    private CancellationTokenSource? _runCts;
    private Task? _receiveTask;
    private Task? _pingTask;
    private Task? _reconnectTask;
    private bool _disposed;
    private bool _manualDisconnect;

    public LicenseWebSocketClient(LicenseClient client)
    {
        _client = client;
    }

    public string SessionId { get; private set; } = string.Empty;

    public bool IsConnected => _socket?.State == WebSocketState.Open;

    public RSA? InstructionPublicKey { get; set; }

    public Func<Task>? OnConnected { get; set; }

    public Func<Exception?, Task>? OnDisconnected { get; set; }

    public Func<Exception, Task>? OnError { get; set; }

    public Func<RealtimeMessage, Task>? OnMessage { get; set; }

    public Func<RealtimeInstruction, Task>? OnInstruction { get; set; }

    public bool AutoReconnect { get; set; }

    public TimeSpan ReconnectInterval { get; set; } = TimeSpan.FromSeconds(5);

    public void RegisterInstructionHandler(string instructionType, Func<RealtimeInstruction, CancellationToken, Task<object?>> handler)
    {
        LicenseClient.EnsureNotEmpty(instructionType, nameof(instructionType));
        ArgumentNullException.ThrowIfNull(handler);
        _handlers[instructionType] = handler;
    }

    public void RegisterInstructionHandler(string instructionType, Func<RealtimeInstruction, object?> handler)
    {
        ArgumentNullException.ThrowIfNull(handler);
        RegisterInstructionHandler(instructionType, (instruction, _) => Task.FromResult(handler(instruction)));
    }

    public async Task ConnectAsync(CancellationToken ct = default)
    {
        ClientWebSocket socket;
        CancellationTokenSource runCts;
        ClientWebSocket? oldSocket;
        CancellationTokenSource? oldRunCts;
        Task? oldReceiveTask;
        Task? oldPingTask;

        await _stateLock.WaitAsync(ct).ConfigureAwait(false);
        try
        {
            ThrowIfDisposed();
            if (IsConnected)
            {
                return;
            }

            oldSocket = _socket;
            oldRunCts = _runCts;
            oldReceiveTask = _receiveTask;
            oldPingTask = _pingTask;
            _socket = null;
            _runCts = null;
            _receiveTask = null;
            _pingTask = null;
            SessionId = string.Empty;
            _manualDisconnect = true;
            oldRunCts?.Cancel();
        }
        finally
        {
            _stateLock.Release();
        }

        await CloseSocketAsync(oldSocket, CancellationToken.None).ConfigureAwait(false);
        await WaitForBackgroundTasksAsync(oldReceiveTask, oldPingTask).ConfigureAwait(false);
        oldRunCts?.Dispose();

        await _stateLock.WaitAsync(ct).ConfigureAwait(false);
        try
        {
            ThrowIfDisposed();
            if (IsConnected)
            {
                return;
            }

            _manualDisconnect = false;
            runCts = CancellationTokenSource.CreateLinkedTokenSource(ct);
            socket = new ClientWebSocket();
            _runCts = runCts;
            _socket = socket;
        }
        finally
        {
            _stateLock.Release();
        }

        try
        {
            var session = _client.Sessions.GetSession();
            if (session is null || session.IsAccessExpired(TokenRefreshSkew))
            {
                session = await _client.RefreshSessionAsync(ct).ConfigureAwait(false);
            }
            if (string.IsNullOrWhiteSpace(session.AccessToken))
            {
                throw new InvalidOperationException("Client access token is required before connecting WebSocket.");
            }
            socket.Options.SetRequestHeader("Authorization", $"{NormalizeTokenType(session.TokenType)} {session.AccessToken}");

            await socket.ConnectAsync(_client.BuildWebSocketUri(), ct).ConfigureAwait(false);
            await SendMessageAsync("auth", new
            {
                app_key = _client.AppKey,
                machine_id = _client.MachineId,
            }, id: null, ct).ConfigureAwait(false);

            using var authCts = CancellationTokenSource.CreateLinkedTokenSource(ct);
            authCts.CancelAfter(AuthTimeout);
            var authResponse = await ReceiveMessageAsync(socket, authCts.Token).ConfigureAwait(false);
            if (authResponse.Type == "error")
            {
                throw new InvalidOperationException("WebSocket auth failed: " + ExtractErrorMessage(authResponse.Payload));
            }
            if (authResponse.Type != "auth_ok")
            {
                throw new InvalidOperationException("WebSocket auth failed: unexpected response " + authResponse.Type);
            }

            if (authResponse.Payload.ValueKind == JsonValueKind.Object &&
                authResponse.Payload.TryGetProperty("session_id", out var sessionId))
            {
                SessionId = sessionId.GetString() ?? string.Empty;
            }

            await _stateLock.WaitAsync(CancellationToken.None).ConfigureAwait(false);
            try
            {
                if (ReferenceEquals(_socket, socket) && ReferenceEquals(_runCts, runCts))
                {
                    _receiveTask = Task.Run(() => ReceiveLoopAsync(runCts.Token), CancellationToken.None);
                    _pingTask = Task.Run(() => PingLoopAsync(runCts.Token), CancellationToken.None);
                }
            }
            finally
            {
                _stateLock.Release();
            }

            if (OnConnected is not null)
            {
                try
                {
                    await OnConnected().ConfigureAwait(false);
                }
                catch (Exception ex)
                {
                    await NotifyErrorAsync(ex).ConfigureAwait(false);
                }
            }
        }
        catch
        {
            runCts.Cancel();
            await _stateLock.WaitAsync(CancellationToken.None).ConfigureAwait(false);
            try
            {
                if (ReferenceEquals(_socket, socket))
                {
                    _socket = null;
                    _runCts = null;
                    _receiveTask = null;
                    _pingTask = null;
                    SessionId = string.Empty;
                }
            }
            finally
            {
                _stateLock.Release();
            }
            socket.Dispose();
            runCts.Dispose();
            throw;
        }
    }

    public async Task DisconnectAsync(CancellationToken ct = default)
    {
        ClientWebSocket? socket;
        CancellationTokenSource? runCts;
        Task? receiveTask;
        Task? pingTask;
        Task? reconnectTask;

        await _stateLock.WaitAsync(ct).ConfigureAwait(false);
        try
        {
            socket = _socket;
            runCts = _runCts;
            receiveTask = _receiveTask;
            pingTask = _pingTask;
            reconnectTask = _reconnectTask;

            _socket = null;
            _runCts = null;
            _receiveTask = null;
            _pingTask = null;
            _reconnectTask = null;
            SessionId = string.Empty;
            _manualDisconnect = true;
            runCts?.Cancel();
        }
        finally
        {
            _stateLock.Release();
        }

        await CloseSocketAsync(socket, ct).ConfigureAwait(false);
        await WaitForBackgroundTasksAsync(receiveTask, pingTask, reconnectTask).ConfigureAwait(false);
        runCts?.Dispose();
    }

    public Task SendStatusAsync(object status, CancellationToken ct = default)
    {
        ArgumentNullException.ThrowIfNull(status);
        return SendMessageAsync("status", status, id: null, ct);
    }

    public Task SendInstructionResultAsync(string instructionId, string status, object? result = null, string? error = null, CancellationToken ct = default)
    {
        LicenseClient.EnsureNotEmpty(instructionId, nameof(instructionId));
        LicenseClient.EnsureNotEmpty(status, nameof(status));
        return SendMessageAsync("instruction_result", new
        {
            instruction_id = instructionId,
            status,
            result,
            error,
        }, instructionId, ct);
    }

    public Task SendScriptResultAsync(string deliveryId, string status, string? result = null, string? error = null, int duration = 0, string? scriptId = null, CancellationToken ct = default)
    {
        LicenseClient.EnsureNotEmpty(deliveryId, nameof(deliveryId));
        LicenseClient.EnsureNotEmpty(status, nameof(status));
        return SendMessageAsync("script_result", new
        {
            script_id = scriptId,
            delivery_id = deliveryId,
            status,
            result,
            error,
            duration,
        }, deliveryId, ct);
    }

    private async Task ReceiveLoopAsync(CancellationToken ct)
    {
        Exception? disconnectError = null;
        try
        {
            while (!ct.IsCancellationRequested && _socket is { State: WebSocketState.Open } socket)
            {
                var message = await ReceiveMessageAsync(socket, ct).ConfigureAwait(false);
                await HandleMessageAsync(message, ct).ConfigureAwait(false);
            }
        }
        catch (OperationCanceledException) when (ct.IsCancellationRequested)
        {
        }
        catch (Exception ex)
        {
            disconnectError = ex;
            if (OnError is not null)
            {
                await NotifyErrorAsync(ex).ConfigureAwait(false);
            }
        }
        finally
        {
            if (OnDisconnected is not null)
            {
                try
                {
                    await OnDisconnected(disconnectError).ConfigureAwait(false);
                }
                catch (Exception ex)
                {
                    await NotifyErrorAsync(ex).ConfigureAwait(false);
                }
            }

            if (AutoReconnect && !_manualDisconnect && !_disposed)
            {
                StartReconnectLoop();
            }
        }
    }

    private void StartReconnectLoop()
    {
        lock (_reconnectLock)
        {
            if (_reconnectTask is { IsCompleted: false })
            {
                return;
            }

            _reconnectTask = Task.Run(ReconnectLoopAsync, CancellationToken.None);
        }
    }

    private async Task ReconnectLoopAsync()
    {
        while (AutoReconnect && !_manualDisconnect && !_disposed)
        {
            try
            {
                await Task.Delay(ReconnectInterval).ConfigureAwait(false);
                if (_manualDisconnect || _disposed || IsConnected)
                {
                    return;
                }

                await ConnectAsync(CancellationToken.None).ConfigureAwait(false);
                return;
            }
            catch (Exception ex)
            {
                await NotifyErrorAsync(ex).ConfigureAwait(false);
            }
        }
    }

    private static async Task CloseSocketAsync(ClientWebSocket? socket, CancellationToken ct)
    {
        if (socket is null)
        {
            return;
        }

        try
        {
            if (socket.State == WebSocketState.Open || socket.State == WebSocketState.CloseReceived)
            {
                await socket.CloseAsync(WebSocketCloseStatus.NormalClosure, "client disconnect", ct).ConfigureAwait(false);
            }
        }
        catch
        {
        }
        finally
        {
            socket.Dispose();
        }
    }

    private async Task PingLoopAsync(CancellationToken ct)
    {
        try
        {
            while (!ct.IsCancellationRequested)
            {
                await Task.Delay(PingInterval, ct).ConfigureAwait(false);
                if (!ct.IsCancellationRequested && IsConnected)
                {
                    await SendMessageAsync("ping", new { ts = DateTimeOffset.UtcNow.ToUnixTimeSeconds() }, id: null, ct).ConfigureAwait(false);
                }
            }
        }
        catch (OperationCanceledException) when (ct.IsCancellationRequested)
        {
        }
        catch (Exception ex)
        {
            await NotifyErrorAsync(ex).ConfigureAwait(false);
        }
    }

    private async Task HandleMessageAsync(RealtimeMessage message, CancellationToken ct)
    {
        if (OnMessage is not null)
        {
            try
            {
                await OnMessage(message).ConfigureAwait(false);
            }
            catch (Exception ex)
            {
                await NotifyErrorAsync(ex).ConfigureAwait(false);
            }
        }

        switch (message.Type)
        {
            case "pong":
                return;
            case "instruction":
                await HandleInstructionAsync(message, ct).ConfigureAwait(false);
                return;
            case "error":
                if (OnError is not null)
                {
                    await NotifyErrorAsync(new InvalidOperationException(ExtractErrorMessage(message.Payload))).ConfigureAwait(false);
                }
                return;
        }
    }

    private async Task HandleInstructionAsync(RealtimeMessage message, CancellationToken ct)
    {
        RealtimeInstruction? instruction;
        try
        {
            instruction = message.Payload.Deserialize<RealtimeInstruction>(LicenseServerClient.JsonOptions);
        }
        catch (Exception ex)
        {
            if (OnError is not null)
            {
                await NotifyErrorAsync(ex).ConfigureAwait(false);
            }
            return;
        }

        if (instruction is null || string.IsNullOrWhiteSpace(instruction.Id))
        {
            return;
        }

        if (instruction.ExpiresAt > 0 && DateTimeOffset.UtcNow.ToUnixTimeSeconds() > instruction.ExpiresAt)
        {
            await SendInstructionResultAsync(instruction.Id, "failed", error: "Instruction expired", ct: ct).ConfigureAwait(false);
            return;
        }

        if (!VerifyInstructionSignature(instruction))
        {
            await SendInstructionResultAsync(instruction.Id, "failed", error: "Instruction signature verification failed", ct: ct).ConfigureAwait(false);
            return;
        }

        if (OnInstruction is not null)
        {
            try
            {
                await OnInstruction(instruction).ConfigureAwait(false);
            }
            catch (Exception ex)
            {
                await NotifyErrorAsync(ex).ConfigureAwait(false);
            }
        }

        if (!_handlers.TryGetValue(instruction.Type, out var handler))
        {
            await SendInstructionResultAsync(instruction.Id, "failed", error: "Unknown instruction type", ct: ct).ConfigureAwait(false);
            return;
        }

        try
        {
            var result = await handler(instruction, ct).ConfigureAwait(false);
            await SendInstructionResultAsync(instruction.Id, "success", result, ct: ct).ConfigureAwait(false);
        }
        catch (Exception ex)
        {
            await SendInstructionResultAsync(instruction.Id, "failed", error: ex.Message, ct: ct).ConfigureAwait(false);
        }
    }

    private bool VerifyInstructionSignature(RealtimeInstruction instruction)
    {
        if (InstructionPublicKey is null)
        {
            return true;
        }
        if (string.IsNullOrWhiteSpace(instruction.Signature))
        {
            return false;
        }

        try
        {
            var data = Encoding.UTF8.GetBytes($"{instruction.Id}:{instruction.Type}:{instruction.Payload.GetRawText()}:{instruction.Nonce}");
            var signature = Convert.FromBase64String(instruction.Signature);
            return InstructionPublicKey.VerifyData(data, signature, HashAlgorithmName.SHA256, RSASignaturePadding.Pkcs1);
        }
        catch
        {
            return false;
        }
    }

    private async Task SendMessageAsync(string type, object? payload, string? id, CancellationToken ct)
    {
        ThrowIfDisposed();
        var socket = _socket;
        if (socket is null || socket.State != WebSocketState.Open)
        {
            throw new InvalidOperationException("WebSocket is not connected.");
        }

        var bytes = JsonSerializer.SerializeToUtf8Bytes(new RealtimeOutboundMessage
        {
            Type = type,
            Id = id,
            Payload = payload,
        }, LicenseServerClient.JsonOptions);

        await _sendLock.WaitAsync(ct).ConfigureAwait(false);
        try
        {
            await socket.SendAsync(bytes, WebSocketMessageType.Text, endOfMessage: true, ct).ConfigureAwait(false);
        }
        finally
        {
            _sendLock.Release();
        }
    }

    private static async Task<RealtimeMessage> ReceiveMessageAsync(ClientWebSocket socket, CancellationToken ct)
    {
        var buffer = new byte[8192];
        using var output = new MemoryStream();

        while (true)
        {
            var result = await socket.ReceiveAsync(buffer, ct).ConfigureAwait(false);
            if (result.MessageType == WebSocketMessageType.Close)
            {
                throw new WebSocketException("WebSocket closed by server.");
            }

            output.Write(buffer, 0, result.Count);
            if (result.EndOfMessage)
            {
                break;
            }
        }

        var message = JsonSerializer.Deserialize<RealtimeMessage>(output.ToArray(), LicenseServerClient.JsonOptions);
        return message ?? throw new InvalidOperationException("WebSocket message cannot be parsed.");
    }

    private static string ExtractErrorMessage(JsonElement payload)
    {
        if (payload.ValueKind == JsonValueKind.Object &&
            payload.TryGetProperty("message", out var message))
        {
            return message.GetString() ?? "WebSocket error.";
        }

        return payload.ValueKind == JsonValueKind.Undefined ? "WebSocket error." : payload.GetRawText();
    }

    private static string NormalizeTokenType(string? tokenType)
        => string.IsNullOrWhiteSpace(tokenType) ? "Bearer" : tokenType.Trim();

    private async Task NotifyErrorAsync(Exception ex)
    {
        if (OnError is null)
        {
            return;
        }

        try
        {
            await OnError(ex).ConfigureAwait(false);
        }
        catch
        {
        }
    }

    private static async Task WaitForBackgroundTasksAsync(params Task?[] tasks)
    {
        var currentTaskId = Task.CurrentId;
        foreach (var task in tasks)
        {
            if (task is null || task.IsCompleted || task.Id == currentTaskId)
            {
                continue;
            }

            try
            {
                await task.ConfigureAwait(false);
            }
            catch
            {
            }
        }
    }

    private void ThrowIfDisposed()
    {
        if (_disposed)
        {
            throw new ObjectDisposedException(nameof(LicenseWebSocketClient));
        }
    }

    public async ValueTask DisposeAsync()
    {
        if (_disposed)
        {
            return;
        }

        _disposed = true;
        await DisconnectAsync(CancellationToken.None).ConfigureAwait(false);
        _runCts?.Dispose();
        _sendLock.Dispose();
        _stateLock.Dispose();
    }
}

public sealed class RealtimeMessage
{
    [JsonPropertyName("type")]
    public string Type { get; set; } = string.Empty;

    [JsonPropertyName("id")]
    public string Id { get; set; } = string.Empty;

    [JsonPropertyName("payload")]
    public JsonElement Payload { get; set; }
}

public sealed class RealtimeInstruction
{
    [JsonPropertyName("id")]
    public string Id { get; set; } = string.Empty;

    [JsonPropertyName("type")]
    public string Type { get; set; } = string.Empty;

    [JsonPropertyName("payload")]
    public JsonElement Payload { get; set; }

    [JsonPropertyName("timestamp")]
    public long Timestamp { get; set; }

    [JsonPropertyName("nonce")]
    public string Nonce { get; set; } = string.Empty;

    [JsonPropertyName("signature")]
    public string Signature { get; set; } = string.Empty;

    [JsonPropertyName("expires")]
    public long ExpiresAt { get; set; }
}

internal sealed class RealtimeOutboundMessage
{
    [JsonPropertyName("type")]
    public string Type { get; set; } = string.Empty;

    [JsonPropertyName("id")]
    public string? Id { get; set; }

    [JsonPropertyName("payload")]
    public object? Payload { get; set; }
}
