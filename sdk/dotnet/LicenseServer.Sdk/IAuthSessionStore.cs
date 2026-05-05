namespace LicenseServer.Sdk;

/// <summary>
/// Storage abstraction for access tokens. UI apps can plug in DPAPI, Keychain, or another secure store.
/// </summary>
public interface IAuthSessionStore
{
    AuthSession? GetSession();

    void Save(AuthSession session);

    void Clear();
}
