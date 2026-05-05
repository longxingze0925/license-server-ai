using System.Net;

namespace LicenseServer.Sdk;

/// <summary>
/// Error returned by license-server or by the HTTP transport.
/// </summary>
public sealed class LicenseServerException : Exception
{
    public LicenseServerException(HttpStatusCode httpStatus, int businessCode, string message, string responseBody)
        : base(message)
    {
        HttpStatus = httpStatus;
        BusinessCode = businessCode;
        ResponseBody = responseBody;
    }

    public LicenseServerException(string message, Exception innerException)
        : base(message, innerException)
    {
        HttpStatus = 0;
        BusinessCode = 0;
        ResponseBody = string.Empty;
    }

    public HttpStatusCode HttpStatus { get; }

    public int BusinessCode { get; }

    public string ResponseBody { get; }

    public bool IsUnauthorized => HttpStatus == HttpStatusCode.Unauthorized;

    public bool IsPaymentRequired => HttpStatus == HttpStatusCode.PaymentRequired;
}
