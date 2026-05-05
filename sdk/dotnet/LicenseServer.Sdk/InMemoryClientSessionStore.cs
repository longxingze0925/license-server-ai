namespace LicenseServer.Sdk;

/// <summary>
/// Simple process-local client session store.
/// </summary>
public sealed class InMemoryClientSessionStore : IClientSessionStore
{
    private readonly object _gate = new();
    private ClientSession? _session;

    public ClientSession? GetSession()
    {
        lock (_gate)
        {
            if (_session is null)
            {
                return null;
            }

            return _session.IsRefreshExpired(TimeSpan.FromSeconds(60)) ? null : _session;
        }
    }

    public void Save(ClientSession session)
    {
        if (string.IsNullOrWhiteSpace(session.AppKey))
        {
            throw new ArgumentException("AppKey cannot be empty.", nameof(session));
        }
        if (string.IsNullOrWhiteSpace(session.MachineId))
        {
            throw new ArgumentException("MachineId cannot be empty.", nameof(session));
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
