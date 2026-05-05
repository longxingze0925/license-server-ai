namespace LicenseServer.Sdk;

/// <summary>
/// Persistence boundary for client-side app sessions.
/// </summary>
public interface IClientSessionStore
{
    ClientSession? GetSession();

    void Save(ClientSession session);

    void Clear();
}
