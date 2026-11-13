package e2e

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"ocm.software/open-component-model/bindings/go/kubernetes/controller/test/utils"
	"ocm.software/open-component-model/bindings/go/oci/looseref"
)

const (
	discoveryBootstrap     = "discovery-bootstrap"
	tamperConstructor      = "tamper-component-constructor.yaml"
	tamperDiscovery        = "tamper-discovery.yaml"
	tamperDiscoveryName    = "discovery-bootstrap-recheck"
	referenceDigestMessage = `does not match the digest recorded by reference "frontend"`
)

// The discovery-bootstrap example bootstraps a signed umbrella component with a single Discovery and a
// NamespacedDeployer. The RGD it ships discovers the charts and images of the child components and deploys
// them by the digests the Discovery verified.
var _ = Describe("Discovery bootstrap", func() {
	It("should deploy the example "+discoveryBootstrap, func(ctx SpecContext) {
		dir := filepath.Join(examplesDir, discoveryBootstrap)
		fInfo, err := os.Stat(dir)
		Expect(err).NotTo(HaveOccurred())
		Expect(fInfo.IsDir()).To(BeTrue())

		DeferCleanup(func() {
			if !CurrentSpecReport().Failed() {
				return
			}
			utils.DumpLogs("default", "discoveries.delivery.ocm.software")
			utils.DumpLogs("kro", "rgd")
		})

		By("creating, signing and transferring the umbrella component and its children")
		Expect(utils.PrepareOCMComponent(ctx, discoveryBootstrap,
			filepath.Join(dir, ComponentConstructor), imageRegistry,
			utils.WithRecursiveTransfer(), utils.WithCopyResources(),
		)).To(Succeed())

		By("bootstrapping with one Discovery and one NamespacedDeployer")
		Expect(utils.DeployResourceWithoutCleanup(ctx, filepath.Join(dir, Bootstrap))).To(Succeed())
		// Delete the NamespacedDeployer while its service account still exists, so it prunes the RGD;
		// deleting it together with the rest of the bootstrap orphans the RGD.
		DeferCleanup(func(ctx SpecContext) error {
			if err := utils.DeleteResource(ctx, timeout,
				"namespaceddeployer.delivery.ocm.software/"+discoveryBootstrap); err != nil {
				return err
			}
			return utils.DeleteResource(ctx, timeout, "-f", filepath.Join(dir, Bootstrap), "--ignore-not-found")
		})
		Expect(utils.WaitForResource(ctx, "condition=Ready=true", timeout,
			"discovery.delivery.ocm.software/"+discoveryBootstrap)).To(Succeed())
		By("validating the Repository and Component generated from the inline source")
		Expect(utils.WaitForResource(ctx, "condition=Ready=true", timeout,
			"repository.delivery.ocm.software/"+discoveryBootstrap+"-repository",
			"component.delivery.ocm.software/"+discoveryBootstrap+"-component")).To(Succeed())
		Expect(utils.WaitForResource(ctx, "condition=Ready=true", timeout,
			"namespaceddeployer.delivery.ocm.software/"+discoveryBootstrap)).To(Succeed())
		Expect(utils.WaitForResource(ctx, "create", timeout, "rgd/"+discoveryBootstrap)).To(Succeed())
		Expect(utils.WaitForResource(ctx, "condition=Ready=true", timeout, "rgd/"+discoveryBootstrap)).To(Succeed())

		By("confirming the controller verified the umbrella signature")
		Eventually(func(g Gomega) string {
			out, err := utils.Run(exec.CommandContext(ctx, "kubectl", "logs",
				"-n", "ocm-k8s-toolkit-system", "-l", "app.kubernetes.io/name=ocm-k8s-toolkit", "--tail=-1"))
			g.Expect(err).NotTo(HaveOccurred())
			return string(out)
		}, timeout).Should(MatchRegexp(`"msg":"verified signature"[^\n]*"signature":"ocm.software.discovery.bootstrap.rsa"`))

		By("deploying the RGD instance")
		Expect(utils.DeployResource(ctx, filepath.Join(dir, Instance))).To(Succeed())
		Expect(utils.WaitForResource(ctx, "condition=Ready=true", timeout,
			"discoverybootstrap.kro.run/"+discoveryBootstrap)).To(Succeed())

		expectedRegistry, err := looseref.ParseReference(imageRegistry + "/")
		Expect(err).NotTo(HaveOccurred())
		for _, app := range []string{"frontend", "backend"} {
			release := discoveryBootstrap + "-" + app
			By("validating the HelmRelease and pod of " + release)
			Expect(utils.WaitForResource(ctx, "condition=Ready=true", timeout,
				"helmrelease.helm.toolkit.fluxcd.io/"+release)).To(Succeed())
			Expect(utils.WaitForResource(ctx, "condition=Available", timeout,
				"deployment.apps/"+release+"-podinfo")).To(Succeed())
			Expect(utils.WaitForResource(ctx, "condition=Ready=true", timeout,
				"pod", "-l", "app.kubernetes.io/name="+release+"-podinfo")).To(Succeed())

			image, err := utils.GetResourceField(ctx, "pod -l app.kubernetes.io/name="+release+"-podinfo",
				"'{.items[0].spec.containers[0].image}'")
			Expect(err).NotTo(HaveOccurred())
			ref, err := looseref.ParseReference(image)
			Expect(err).NotTo(HaveOccurred(), "container image %q is not a valid OCI reference", image)
			Expect(ref.Registry).To(Equal(expectedRegistry.Registry), "image %q is not localized", image)

			digest, err := utils.GetResourceField(ctx, "discovery.delivery.ocm.software/"+discoveryBootstrap+"-payload",
				`'{.status.resources[?(@.resource.type=="ociImage")]}'`)
			Expect(err).NotTo(HaveOccurred())
			Expect(image).To(MatchRegexp(`@sha256:[0-9a-f]{64}$`), "image %q is not pinned by digest", image)
			Expect(digest).To(ContainSubstring(image[strings.LastIndex(image, "@")+1:]),
				"image %q is not pinned to a digest the Discovery verified", image)
		}

		By("republishing the frontend component at the same version without re-signing the umbrella")
		tmp := GinkgoT().TempDir()
		plainHTTP, err := utils.PlainHTTPConfig(tmp)
		Expect(err).NotTo(HaveOccurred())
		addCmd := exec.CommandContext(ctx, utils.OCMBinary(), "add", "cv",
			"--repository", imageRegistry,
			"--constructor", tamperConstructor,
			"--config", plainHTTP,
		)
		addCmd.Dir = dir
		_, err = utils.Run(addCmd)
		Expect(err).NotTo(HaveOccurred())

		By("a fresh Discovery of the same umbrella fails closed on the reference digest")
		Expect(utils.DeployResource(ctx, filepath.Join(dir, tamperDiscovery))).To(Succeed())
		Eventually(func(g Gomega) string {
			msg, err := utils.GetResourceField(ctx, "discovery.delivery.ocm.software/"+tamperDiscoveryName,
				`'{.status.conditions[?(@.type=="Ready")].status}{" "}{.status.conditions[?(@.type=="Ready")].message}'`)
			g.Expect(err).NotTo(HaveOccurred())
			return msg
		}, timeout).Should(And(HavePrefix("False "), ContainSubstring(referenceDigestMessage)))

		By("the running application stays on the verified version")
		Expect(utils.WaitForResource(ctx, "condition=Ready=true", timeout,
			"discovery.delivery.ocm.software/"+discoveryBootstrap+"-payload",
			"helmrelease.helm.toolkit.fluxcd.io/"+discoveryBootstrap+"-frontend")).To(Succeed())
	})
})
