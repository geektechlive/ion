# Code Signing Setup Guide

This guide walks through setting up Apple code signing for Ion's CI pipeline.

## Prerequisites

- Apple Developer Program membership
- Admin access to the GitHub repository

## Step 1: Developer ID Application Certificate

1. Open Keychain Access on your Mac
2. In the menu: Keychain Access → Certificate Assistant → Request a Certificate From a Certificate Authority
3. Go to [Apple Developer > Certificates](https://developer.apple.com/account/resources/certificates/list)
4. Click "+" → "Developer ID Application"
5. Upload the certificate signing request
6. Download and install the certificate

### Export as .p12

1. In Keychain Access, find "Developer ID Application: Your Name"
2. Right-click → Export
3. Choose .p12 format, set a strong password
4. Base64-encode: `base64 -i certificate.p12 | pbcopy`

## Step 2: App Store Connect API Key

1. Go to [App Store Connect > Users and Access > Integrations > Keys](https://appstoreconnect.apple.com/access/integrations/api)
2. Click "+" to generate a new key
3. Give it "Developer" access
4. Download the .p8 file (you can only download it once)
5. Note the Key ID and Issuer ID
6. Base64-encode: `base64 -i AuthKey_XXXXXX.p8 | pbcopy`

## Step 3: GitHub Secrets

Go to your repo's Settings → Secrets and variables → Actions, add:

| Secret | Value |
|--------|-------|
| `APPLE_CERT_BASE64` | Base64 of the .p12 file |
| `APPLE_CERT_PASSWORD` | The password you set when exporting |
| `APPLE_API_KEY` | Base64 of the .p8 file |
| `APPLE_API_KEY_ID` | Key ID from App Store Connect |
| `APPLE_API_ISSUER` | Issuer UUID from App Store Connect |

## Step 4: Verify

Push a release and watch the CI pipeline. The darwin builds should:

1. Import the certificate into a temporary keychain
2. Sign the binary with "Developer ID Application: ..."
3. Submit to notarytool and wait for approval
4. Clean up the keychain

## Local Development

No signing setup is needed for local development. The Makefile uses ad-hoc signing, and electron-builder falls back gracefully when no Developer ID certificate is found.

## Troubleshooting

### "The specified item could not be found in the keychain"

The certificate import failed. Check that `APPLE_CERT_BASE64` is correctly encoded and `APPLE_CERT_PASSWORD` matches.

### "Unable to process the request... The operation couldn't be completed"

The notarytool API key is invalid. Verify `APPLE_API_KEY`, `APPLE_API_KEY_ID`, and `APPLE_API_ISSUER`.

### "Package Invalid" from notarytool

The binary wasn't signed with hardened runtime. Ensure `--options runtime` is in the codesign command.
