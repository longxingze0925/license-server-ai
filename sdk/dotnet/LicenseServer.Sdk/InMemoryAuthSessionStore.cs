namespace LicenseServer.Sdk;

/// <summary>
/// Simple process-local token store, useful for tests, console tools, and apps that manage persistence elsewhere.
/// </summary>
public sealed class InMemoryAuthSessionStore : IAuthSessionStore
{
    private readonly object _gate = new();
    private AuthSession? _session;

    public AuthSession? GetSession()
    {
        lock (_gate)
        {
            if (_session is null)
            {
                return null;
            }

            return _session.IsExpired(TimeSpan.FromSeconds(60)) ? null : _session;
        }
    }

    public void Save(AuthSession session)
    {
        if (string.IsNullOrWhiteSpace(session.Token))
        {
            throw new ArgumentException("Token cannot be empty.", nameof(session));
        }

        lock (_gate)
        {
            _session = session;
        }
    }

    public void Clear()
    {
        lock (_gate)
        {
            _session = null;
        }
    }
}
