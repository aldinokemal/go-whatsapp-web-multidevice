# Official GOWA SDKs

New SDK packages follow `info.version` in the OpenAPI document shipped with
your GOWA release. Check that version and select its matching SDK package.
The legacy SDKs published for GOWA v9.11.0 contain OpenAPI 9.7.0 and use the
following install commands:

| SDK | Install |
| --- | --- |
| [Go](https://github.com/aldinokemal/gowa-sdk-go) | `go get github.com/aldinokemal/gowa-sdk-go/v9@v9.11.0` |
| [Node / TypeScript](https://github.com/aldinokemal/gowa-sdk-node) | `npm install @aldinokemal2104/gowa-sdk-node@9.11.0` |
| [PHP](https://github.com/aldinokemal/gowa-sdk-php) | `composer require aldinokemal/gowa-sdk-php:9.11.0` |

PHP keeps the `SdkWhatsappWebMultiDevice` namespace. The Go import path includes
the OpenAPI major version. Node and PHP consumers using an older SDK should
review generated method signatures when moving to GOWA 9.

## Automatic releases

After both GOWA binary release jobs succeed for a stable tag, GitHub Actions
sends the tag and its commit SHA to each SDK repository. Each SDK generates from
that exact `docs/openapi.yaml` when the OpenAPI version increases. Each workflow
skips versions already covered by its SDK. Go publishes a module tag, Node
publishes to npm, and PHP publishes a tag for Composer. Configure automatic
Packagist updates in the PHP repository.

During the transition, pin new packages explicitly if their OpenAPI version
sorts below the legacy SDK version `9.11.0`. Node puts those releases on
npm's `openapi` dist-tag.

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
