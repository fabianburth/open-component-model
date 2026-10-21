package v4alpha2_test

// Conformance fixture kept in code (no testdata/embed), mirroring
// jsonNormalisation/v1's fixtures_test.go. input is the stored component
// descriptor as readable YAML; norm is the expected v4alpha2 normalised bytes
// (byte-exact).
//
// Unlike the v1 fixtures, these expected bytes have no external anchor: they
// are DEFINED by this implementation, so there is no separate sha256 digest to
// cross-check (that would be circular). The fixture's job is to pin the bytes
// so an accidental change to the algorithm is caught.
//
// input_cli is the v0.27.0 ocmcli descriptor (`ocm get cv
// ghcr.io/open-component-model/ocm//ocm.software/ocmcli:0.27.0 -oyaml`), the
// same stored descriptor used by the v4alpha1/v3 conformance fixture. The
// expected bytes differ from v4alpha1: the per-resource downloadName labels
// carry no signing attribute and are kept under v4alpha2's default-signed rule
// (Option 2), whereas v4alpha1 dropped them.
const (
	input_cli = `
component:
  componentReferences: []
  creationTime: "2025-07-28T11:40:51Z"
  name: ocm.software/ocmcli
  provider: ocm.software
  repositoryContexts:
    - baseUrl: ghcr.io
      componentNameMapping: urlPath
      subPath: open-component-model/ocm
      type: OCIRegistry
  resources:
    - access:
        localReference: sha256:64b586a57294adc5749324d0574df23499555de5168b4b1f1bd7ce9b06e2d49f
        mediaType: application/octet-stream
        type: localBlob
      digest:
        hashAlgorithm: SHA-256
        normalisationAlgorithm: genericBlobDigest/v1
        value: 64b586a57294adc5749324d0574df23499555de5168b4b1f1bd7ce9b06e2d49f
      extraIdentity:
        architecture: amd64
        os: windows
      labels:
        - name: downloadName
          value: ocm
      name: ocmcli
      relation: local
      type: executable
      version: 0.27.0
    - access:
        localReference: sha256:f038414e1ed095535e69b06c712e3c1a26f3fc399419721fdc8bf3c89ca499dd
        mediaType: application/octet-stream
        type: localBlob
      digest:
        hashAlgorithm: SHA-256
        normalisationAlgorithm: genericBlobDigest/v1
        value: f038414e1ed095535e69b06c712e3c1a26f3fc399419721fdc8bf3c89ca499dd
      extraIdentity:
        architecture: arm64
        os: darwin
      labels:
        - name: downloadName
          value: ocm
      name: ocmcli
      relation: local
      type: executable
      version: 0.27.0
    - access:
        localReference: sha256:6e244b50ce871e97b93c316907d75984d9828e6a55fb29e881495f6fd824ed15
        mediaType: application/octet-stream
        type: localBlob
      digest:
        hashAlgorithm: SHA-256
        normalisationAlgorithm: genericBlobDigest/v1
        value: 6e244b50ce871e97b93c316907d75984d9828e6a55fb29e881495f6fd824ed15
      extraIdentity:
        architecture: amd64
        os: darwin
      labels:
        - name: downloadName
          value: ocm
      name: ocmcli
      relation: local
      type: executable
      version: 0.27.0
    - access:
        localReference: sha256:1fb6cf9c9283497621ca7a30e74df64848c0d93fd74c953e700e8bb6b4054203
        mediaType: application/octet-stream
        type: localBlob
      digest:
        hashAlgorithm: SHA-256
        normalisationAlgorithm: genericBlobDigest/v1
        value: 1fb6cf9c9283497621ca7a30e74df64848c0d93fd74c953e700e8bb6b4054203
      extraIdentity:
        architecture: amd64
        os: linux
      labels:
        - name: downloadName
          value: ocm
      name: ocmcli
      relation: local
      type: executable
      version: 0.27.0
    - access:
        localReference: sha256:90d3a82557315ce9798075e769d52a8ea4352b4009526c7dd9b593f10fba4d77
        mediaType: application/octet-stream
        type: localBlob
      digest:
        hashAlgorithm: SHA-256
        normalisationAlgorithm: genericBlobDigest/v1
        value: 90d3a82557315ce9798075e769d52a8ea4352b4009526c7dd9b593f10fba4d77
      extraIdentity:
        architecture: arm64
        os: linux
      labels:
        - name: downloadName
          value: ocm
      name: ocmcli
      relation: local
      type: executable
      version: 0.27.0
    - access:
        imageReference: ghcr.io/open-component-model/ocm/ocm.software/ocmcli/ocmcli-image:0.27.0@sha256:b91854cb7e2b73decd197ad1408cb09fe6a2af8b289eda9030eb75a722ffef63
        type: ociArtifact
      digest:
        hashAlgorithm: SHA-256
        normalisationAlgorithm: ociArtifactDigest/v1
        value: b91854cb7e2b73decd197ad1408cb09fe6a2af8b289eda9030eb75a722ffef63
      name: ocmcli-image
      relation: local
      type: ociImage
      version: 0.27.0
  sources:
    - access:
        commit: 3ff8ed0e7386aa6fe19dcbbb8955f34a88745fd0
        repoUrl: github.com/open-component-model/ocm
        type: github
      name: source
      type: filesytem
      version: 0.27.0
  version: 0.27.0
meta:
  schemaVersion: v2
`
	norm_cli = `{"component":{"componentReferences":[],"creationTime":"2025-07-28T11:40:51Z","name":"ocm.software/ocmcli","provider":{"name":"ocm.software"},"resources":[{"digest":{"hashAlgorithm":"SHA-256","normalisationAlgorithm":"genericBlobDigest/v1","value":"64b586a57294adc5749324d0574df23499555de5168b4b1f1bd7ce9b06e2d49f"},"extraIdentity":{"architecture":"amd64","os":"windows"},"labels":[{"name":"downloadName","value":"ocm"}],"name":"ocmcli","relation":"local","type":"executable","version":"0.27.0"},{"digest":{"hashAlgorithm":"SHA-256","normalisationAlgorithm":"genericBlobDigest/v1","value":"f038414e1ed095535e69b06c712e3c1a26f3fc399419721fdc8bf3c89ca499dd"},"extraIdentity":{"architecture":"arm64","os":"darwin"},"labels":[{"name":"downloadName","value":"ocm"}],"name":"ocmcli","relation":"local","type":"executable","version":"0.27.0"},{"digest":{"hashAlgorithm":"SHA-256","normalisationAlgorithm":"genericBlobDigest/v1","value":"6e244b50ce871e97b93c316907d75984d9828e6a55fb29e881495f6fd824ed15"},"extraIdentity":{"architecture":"amd64","os":"darwin"},"labels":[{"name":"downloadName","value":"ocm"}],"name":"ocmcli","relation":"local","type":"executable","version":"0.27.0"},{"digest":{"hashAlgorithm":"SHA-256","normalisationAlgorithm":"genericBlobDigest/v1","value":"1fb6cf9c9283497621ca7a30e74df64848c0d93fd74c953e700e8bb6b4054203"},"extraIdentity":{"architecture":"amd64","os":"linux"},"labels":[{"name":"downloadName","value":"ocm"}],"name":"ocmcli","relation":"local","type":"executable","version":"0.27.0"},{"digest":{"hashAlgorithm":"SHA-256","normalisationAlgorithm":"genericBlobDigest/v1","value":"90d3a82557315ce9798075e769d52a8ea4352b4009526c7dd9b593f10fba4d77"},"extraIdentity":{"architecture":"arm64","os":"linux"},"labels":[{"name":"downloadName","value":"ocm"}],"name":"ocmcli","relation":"local","type":"executable","version":"0.27.0"},{"digest":{"hashAlgorithm":"SHA-256","normalisationAlgorithm":"ociArtifactDigest/v1","value":"b91854cb7e2b73decd197ad1408cb09fe6a2af8b289eda9030eb75a722ffef63"},"name":"ocmcli-image","relation":"local","type":"ociImage","version":"0.27.0"}],"sources":[{"name":"source","type":"filesytem","version":"0.27.0"}],"version":"0.27.0"}}`
)
