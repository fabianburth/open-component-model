# OCM v0.27.0 - JSON Normalisation v4alpha2 OCM CLI Component Version

## Input

Same stored descriptor as the v4alpha1/v3 conformance fixture (`ocm get cv
ghcr.io/open-component-model/ocm//ocm.software/ocmcli:0.27.0 -oyaml`).

## Expected

The expected bytes differ from v4alpha1: the per-resource `downloadName` labels
carry no `signing` attribute and are therefore kept under v4alpha2's
default-signed rule (Option 2), whereas v4alpha1 dropped them.
