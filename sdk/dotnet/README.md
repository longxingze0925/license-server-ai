# LicenseServer .NET SDK

This SDK is the standalone .NET version of the C# HTTP wrapper used by the WPF client.
It does not depend on WPF or AiVideoStudio assemblies.

## What It Covers

- `/api/auth/login`
- `/api/auth/profile`
- `/api/credits/me`
- `/api/credits/me/transactions`
- `/api/proxy/capabilities`
- `/api/proxy/{provider}/chat`
- `/api/proxy/{provider}/generate`
- `/api/proxy/tasks`
- `/api/proxy/tasks/{id}`
- `/api/proxy/files`
- `/api/proxy/files/{id}`

The SDK is still HTTP based. It wraps request construction, Bearer token injection,
license-server response parsing, unauthorized-session clearing, and proxy DTOs.

The client-side `LicenseClient` also wraps `/api/client/*` app sessions. Use
`VerifyAsync()` and `SendHeartbeatAsync()` for normal clients; they choose the
license-code or subscription endpoint from the saved session mode.

## Basic Usage

```csharp
using LicenseServer.Sdk;

var sessions = new InMemoryAuthSessionStore();
using var client = new LicenseServerClient(sessions: sessions);

var auth = new AuthApi(client);
var session = await auth.LoginAsync(
    "http://127.0.0.1:8081",
    "admin@example.com",
    Environment.GetEnvironmentVariable("INIT_ADMIN_PASSWORD") ?? "your-admin-password");

var credit = new CreditApi(client);
var balance = await credit.GetMyCreditAsync();

var proxy = new ProxyApi(client);
var capabilities = await proxy.GetCapabilitiesAsync();
```

## Proxy Generate

```csharp
var result = await proxy.GenerateAsync(
    providerSlug: "sora",
    body: new
    {
        model = "sora-2",
        prompt = "a cat dancing under stars",
        duration_seconds = 5,
        aspect_ratio = "16:9",
    },
    mode: "async",
    scope: "video");

var task = await proxy.GetTaskAsync(result.TaskId);
```

## Token Storage

`InMemoryAuthSessionStore` is intentionally simple. Desktop apps should implement
`IAuthSessionStore` with their own secure storage, for example Windows DPAPI.
