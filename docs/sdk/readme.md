# Official GOWA SDKs

Use the SDK version that matches your GOWA server. For GOWA v9.11.0, the public
install commands after the first release are:

| SDK | Install |
| --- | --- |
| [Go](https://github.com/aldinokemal/gowa-sdk-go) | `go get github.com/aldinokemal/gowa-sdk-go/v9@v9.11.0` |
| [Node / TypeScript](https://github.com/aldinokemal/gowa-sdk-node) | `npm install @aldinokemal2104/gowa-sdk-node@9.11.0` |
| [PHP](https://github.com/aldinokemal/gowa-sdk-php) | `composer require aldinokemal/gowa-sdk-php:9.11.0` or `npm install @aldinokemal2104/gowa-sdk-php@9.11.0` |

The npm PHP package contains PHP source. Use Composer to install its PHP runtime
dependencies. PHP keeps the `SdkWhatsappWebMultiDevice` namespace. The Go import path includes
the server's major version. Node and PHP consumers using an older SDK should
review generated method signatures when moving to GOWA 9.

## Automatic releases

After both GOWA binary release jobs succeed for a stable tag, GitHub Actions
sends the tag and its commit SHA to each SDK repository. Each SDK generates from
that exact `docs/openapi.yaml`, validates the result, and publishes through its
own workflow. Go publishes a Git tag, Node publishes to public npm, and PHP
publishes a Git tag and an npm package. Configure Packagist updates in the PHP repository.

Install a GitHub App under `aldinokemal` with repository Contents write access
and limit the installation to `gowa-sdk-go`, `gowa-sdk-node`, and `gowa-sdk-php`.
Set `SDK_RELEASE_APP_CLIENT_ID` as a GOWA Actions variable and
`SDK_RELEASE_APP_PRIVATE_KEY` as a GOWA Actions secret. The dispatch job mints a
short-lived token separately for each SDK repository. Each SDK's README
documents any registry setup. GOWA's binary release can run before the SDK
dispatcher is configured. Its SDK job writes a warning and skips dispatch in
that case.

Run **Release SDKs** in this repository to dispatch a previously published
stable GOWA release. For a validation run that does not publish, run the
individual SDK's release workflow with its publish option unchecked.
