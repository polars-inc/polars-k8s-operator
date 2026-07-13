//go:build e2e
// +build e2e

package e2e

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"polars-inc/k8s-operator/test/utils"
)

// namespace where the project is deployed in
const namespace = "k8s-operator-system"

const clusterNamespace = "k8s-operator-e2e-clusters"

// serviceAccountName created for the project
const serviceAccountName = "k8s-operator-controller-manager"

// metricsServiceName is the name of the metrics service of the project
const metricsServiceName = "k8s-operator-controller-manager-metrics-service"

// metricsRoleBindingName is the name of the RBAC that will be created to allow get the metrics data
const metricsRoleBindingName = "k8s-operator-metrics-binding"

var _ = Describe("Manager", Ordered, func() {
	var controllerPodName string

	// Before running the tests, set up the environment by creating the namespace,
	// enforce the restricted security policy to the namespace, installing CRDs,
	// and deploying the controller.
	BeforeAll(func() {
		By("creating manager namespace")
		cmd := exec.Command("kubectl", "create", "ns", namespace)
		_, err := utils.Run(cmd)
		Expect(err).NotTo(HaveOccurred(), "Failed to create namespace")

		By("labeling the manager namespace to enforce the restricted security policy")
		cmd = exec.Command("kubectl", "label", "--overwrite", "ns", namespace,
			"pod-security.kubernetes.io/enforce=restricted")
		_, err = utils.Run(cmd)
		Expect(err).NotTo(HaveOccurred(), "Failed to label namespace with restricted policy")

		By("creating the namespace for PolarsClusters")
		cmd = exec.Command("kubectl", "create", "ns", clusterNamespace)
		_, err = utils.Run(cmd)
		Expect(err).NotTo(HaveOccurred(), "Failed to create cluster namespace")

		By("installing CRDs")
		cmd = exec.Command("make", "install")
		_, err = utils.Run(cmd)
		Expect(err).NotTo(HaveOccurred(), "Failed to install CRDs")

		By("deploying the controller-manager")
		cmd = exec.Command("make", "deploy", fmt.Sprintf("IMG=%s", managerImage))
		_, err = utils.Run(cmd)
		Expect(err).NotTo(HaveOccurred(), "Failed to deploy the controller-manager")
	})

	// After all tests have been executed, clean up by undeploying the controller, uninstalling CRDs,
	// and deleting the namespace.
	AfterAll(func() {
		By("cleaning up the curl pod for metrics")
		cmd := exec.Command("kubectl", "delete", "pod", "curl-metrics", "-n", namespace)
		_, _ = utils.Run(cmd)

		By("undeploying the controller-manager")
		cmd = exec.Command("make", "undeploy")
		_, _ = utils.Run(cmd)

		By("uninstalling CRDs")
		cmd = exec.Command("make", "uninstall")
		_, _ = utils.Run(cmd)

		By("removing the PolarsCluster namespace")
		cmd = exec.Command("kubectl", "delete", "ns", clusterNamespace)
		_, _ = utils.Run(cmd)

		By("removing manager namespace")
		cmd = exec.Command("kubectl", "delete", "ns", namespace)
		_, _ = utils.Run(cmd)
	})

	// After each test, check for failures and collect logs, events,
	// and pod descriptions for debugging.
	AfterEach(func() {
		specReport := CurrentSpecReport()
		if specReport.Failed() {
			By("Fetching controller manager pod logs")
			cmd := exec.Command("kubectl", "logs", controllerPodName, "-n", namespace)
			controllerLogs, err := utils.Run(cmd)
			if err == nil {
				_, _ = fmt.Fprintf(GinkgoWriter, "Controller logs:\n %s", controllerLogs)
			} else {
				_, _ = fmt.Fprintf(GinkgoWriter, "Failed to get Controller logs: %s", err)
			}

			By("Fetching Kubernetes events")
			for _, ns := range []string{namespace, clusterNamespace} {
				cmd = exec.Command("kubectl", "get", "events", "-n", ns, "--sort-by=.lastTimestamp")
				eventsOutput, err := utils.Run(cmd)
				if err == nil {
					_, _ = fmt.Fprintf(GinkgoWriter, "Kubernetes events in %s:\n%s", ns, eventsOutput)
				} else {
					_, _ = fmt.Fprintf(GinkgoWriter, "Failed to get Kubernetes events in %s: %s", ns, err)
				}
			}

			By("Fetching curl-metrics logs")
			cmd = exec.Command("kubectl", "logs", "curl-metrics", "-n", namespace)
			metricsOutput, err := utils.Run(cmd)
			if err == nil {
				_, _ = fmt.Fprintf(GinkgoWriter, "Metrics logs:\n %s", metricsOutput)
			} else {
				_, _ = fmt.Fprintf(GinkgoWriter, "Failed to get curl-metrics logs: %s", err)
			}

			By("Fetching controller manager pod description")
			cmd = exec.Command("kubectl", "describe", "pod", controllerPodName, "-n", namespace)
			podDescription, err := utils.Run(cmd)
			if err == nil {
				fmt.Println("Pod description:\n", podDescription)
			} else {
				fmt.Println("Failed to describe controller pod")
			}
		}
	})

	SetDefaultEventuallyTimeout(2 * time.Minute)
	SetDefaultEventuallyPollingInterval(time.Second)

	Context("Manager", func() {
		It("should run successfully", func() {
			By("validating that the controller-manager pod is running as expected")
			verifyControllerUp := func(g Gomega) {
				By("getting the name of the controller-manager pod")
				cmd := exec.Command("kubectl", "get",
					"pods", "-l", "control-plane=controller-manager",
					"-o", "go-template={{ range .items }}"+
						"{{ if not .metadata.deletionTimestamp }}"+
						"{{ .metadata.name }}"+
						"{{ \"\\n\" }}{{ end }}{{ end }}",
					"-n", namespace,
				)

				podOutput, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred(), "Failed to retrieve controller-manager pod information")
				podNames := utils.GetNonEmptyLines(podOutput)
				g.Expect(podNames).To(HaveLen(1), "expected 1 controller pod running")
				controllerPodName = podNames[0]
				g.Expect(controllerPodName).To(ContainSubstring("controller-manager"))

				By("validating the pod's status")
				cmd = exec.Command("kubectl", "get",
					"pods", controllerPodName, "-o", "jsonpath={.status.phase}",
					"-n", namespace,
				)
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).To(Equal("Running"), "Incorrect controller-manager pod status")
			}
			Eventually(verifyControllerUp).Should(Succeed())
		})

		It("should ensure the metrics endpoint is serving metrics", func() {
			By("creating a ClusterRoleBinding for the service account to allow access to metrics")
			cmd := exec.Command("kubectl", "create", "clusterrolebinding", metricsRoleBindingName,
				"--clusterrole=k8s-operator-metrics-reader",
				fmt.Sprintf("--serviceaccount=%s:%s", namespace, serviceAccountName),
			)
			_, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred(), "Failed to create ClusterRoleBinding")

			By("validating that the metrics service is available")
			cmd = exec.Command("kubectl", "get", "service", metricsServiceName, "-n", namespace)
			_, err = utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred(), "Metrics service should exist")

			By("getting the service account token")
			token, err := serviceAccountToken()
			Expect(err).NotTo(HaveOccurred())
			Expect(token).NotTo(BeEmpty())

			By("ensuring the controller pod is ready")
			verifyControllerPodReady := func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "pod", controllerPodName, "-n", namespace,
					"-o", "jsonpath={.status.conditions[?(@.type=='Ready')].status}")
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).To(Equal("True"), "Controller pod not ready")
			}
			Eventually(verifyControllerPodReady, 3*time.Minute, time.Second).Should(Succeed())

			By("verifying that the controller manager is serving the metrics server")
			verifyMetricsServerStarted := func(g Gomega) {
				cmd := exec.Command("kubectl", "logs", controllerPodName, "-n", namespace)
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).To(ContainSubstring("Serving metrics server"),
					"Metrics server not yet started")
			}
			Eventually(verifyMetricsServerStarted, 3*time.Minute, time.Second).Should(Succeed())

			// +kubebuilder:scaffold:e2e-metrics-webhooks-readiness

			By("creating the curl-metrics pod to access the metrics endpoint")
			cmd = exec.Command("kubectl", "run", "curl-metrics", "--restart=Never",
				"--namespace", namespace,
				"--image=curlimages/curl:latest",
				"--overrides",
				fmt.Sprintf(`{
					"spec": {
						"containers": [{
							"name": "curl",
							"image": "curlimages/curl:latest",
							"command": ["/bin/sh", "-c"],
							"args": [
								"for i in $(seq 1 30); do curl -v -k -H 'Authorization: Bearer %s' https://%s.%s.svc.cluster.local:8443/metrics && exit 0 || sleep 2; done; exit 1"
							],
							"securityContext": {
								"readOnlyRootFilesystem": true,
								"allowPrivilegeEscalation": false,
								"capabilities": {
									"drop": ["ALL"]
								},
								"runAsNonRoot": true,
								"runAsUser": 1000,
								"seccompProfile": {
									"type": "RuntimeDefault"
								}
							}
						}],
						"serviceAccountName": "%s"
					}
				}`, token, metricsServiceName, namespace, serviceAccountName))
			_, err = utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred(), "Failed to create curl-metrics pod")

			By("waiting for the curl-metrics pod to complete.")
			verifyCurlUp := func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "pods", "curl-metrics",
					"-o", "jsonpath={.status.phase}",
					"-n", namespace)
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).To(Equal("Succeeded"), "curl pod in wrong status")
			}
			Eventually(verifyCurlUp, 5*time.Minute).Should(Succeed())

			By("getting the metrics by checking curl-metrics logs")
			verifyMetricsAvailable := func(g Gomega) {
				metricsOutput, err := getMetricsOutput()
				g.Expect(err).NotTo(HaveOccurred(), "Failed to retrieve logs from curl pod")
				g.Expect(metricsOutput).NotTo(BeEmpty())
				g.Expect(metricsOutput).To(ContainSubstring("< HTTP/1.1 200 OK"))
			}
			Eventually(verifyMetricsAvailable, 2*time.Minute).Should(Succeed())
		})

		// +kubebuilder:scaffold:e2e-webhooks-checks

		itReconcilesWorkerPool(busyboxSpecConfig, polarsClusterManifest(clusterNamespace, e2ePolarsClusterName, 2), 2)

		itCreatesAndReadiesSchedulerPod(busyboxSpecConfig)

		It("should reflect computed env vars on the scheduler pod", func() {
			schedulerPod := schedulerPodNameForCluster(e2ePolarsClusterName)

			By("verifying always-on computed env vars")
			rustBacktrace, err := containerEnvValue(clusterNamespace, schedulerPod, "RUST_BACKTRACE")
			Expect(err).NotTo(HaveOccurred())
			Expect(rustBacktrace).To(Equal("full"))

			schedulerEnabled, err := containerEnvValue(clusterNamespace, schedulerPod, "PC_CUBLET__scheduler__enabled")
			Expect(err).NotTo(HaveOccurred())
			Expect(schedulerEnabled).To(Equal("true"))

			By("verifying the license env vars were injected from the license fieldRef config")
			cmd := exec.Command("kubectl", "get", "pod", schedulerPod, "-n", clusterNamespace,
				"-o", "jsonpath={.spec.containers[0].env[?(@.name=='PC_CUBLET__license__on_prem__client_id')].valueFrom.fieldRef.fieldPath}")
			output, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())
			Expect(output).To(Equal("metadata.name"))

			By("verifying the anonymous-results S3 endpoint was reflected from spec.anonymousResults")
			anonymousResultsURL, err := containerEnvValue(clusterNamespace, schedulerPod, "PC_CUBLET__scheduler__anonymous_result_location__s3__url")
			Expect(err).NotTo(HaveOccurred())
			Expect(anonymousResultsURL).To(Equal(anonymousResultsS3Endpoint))
		})

		It("should reflect computed env vars and volumes on worker pods", func() {
			selector, err := selectorForWorkerPool(clusterNamespace, e2ePolarsClusterName)
			Expect(err).NotTo(HaveOccurred())
			names, err := podNamesForSelector(clusterNamespace, selector)
			Expect(err).NotTo(HaveOccurred())
			Expect(names).NotTo(BeEmpty())
			workerPod := names[0]

			By("verifying always-on computed env vars")
			rustBacktrace, err := containerEnvValue(clusterNamespace, workerPod, "RUST_BACKTRACE")
			Expect(err).NotTo(HaveOccurred())
			Expect(rustBacktrace).To(Equal("full"))

			workerEnabled, err := containerEnvValue(clusterNamespace, workerPod, "PC_CUBLET__worker__enabled")
			Expect(err).NotTo(HaveOccurred())
			Expect(workerEnabled).To(Equal("true"))

			By("verifying the default (unconfigured) shuffle-data location env var")
			shuffleLocalPath, err := containerEnvValue(clusterNamespace, workerPod, "PC_CUBLET__worker__shuffle_location__local__path")
			Expect(err).NotTo(HaveOccurred())
			Expect(shuffleLocalPath).To(Equal("/app/shuffle_data/shuffle_data"))

			By("verifying the always-present temporary-data volume is mounted")
			cmd := exec.Command("kubectl", "get", "pod", workerPod, "-n", clusterNamespace,
				"-o", "jsonpath={.spec.containers[0].volumeMounts[?(@.name=='temporary-data')].mountPath}")
			output, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())
			Expect(output).To(Equal("/app/temporary_data"))

			By("verifying the checkpoint-data S3 endpoint was reflected from spec.checkpointData")
			checkpointURL, err := containerEnvValue(clusterNamespace, workerPod, "PC_CUBLET__worker__checkpoint_location__s3__url")
			Expect(err).NotTo(HaveOccurred())
			Expect(checkpointURL).To(Equal(checkpointDataS3Endpoint))
		})

		itSetsOverallReadyCondition(busyboxSpecConfig)

		It("should create the scheduler, internal, and observatory Services", func() {
			expected := map[string]string{
				e2ePolarsClusterName + "-scheduler":          "5051",
				e2ePolarsClusterName + "-scheduler-internal": "5050 5049",
				e2ePolarsClusterName + "-observatory":        "3001",
			}

			for name, ports := range expected {
				Eventually(func(g Gomega) {
					cmd := exec.Command("kubectl", "get", "service", name, "-n", clusterNamespace,
						"-o", "jsonpath={.spec.ports[*].port}")
					output, err := utils.Run(cmd)
					g.Expect(err).NotTo(HaveOccurred())
					g.Expect(output).To(Equal(ports))
				}).Should(Succeed())
			}
		})

		It("should point worker pods at the internal Service hostname", func() {
			selector, err := selectorForWorkerPool(clusterNamespace, e2ePolarsClusterName)
			Expect(err).NotTo(HaveOccurred())
			names, err := podNamesForSelector(clusterNamespace, selector)
			Expect(err).NotTo(HaveOccurred())
			Expect(names).NotTo(BeEmpty())

			hostname, err := containerEnvValue(clusterNamespace, names[0], "PC_CUBLET__static_leader__scheduler_service__public_addr__hostname")
			Expect(err).NotTo(HaveOccurred())
			Expect(hostname).To(Equal(fmt.Sprintf("%s-scheduler-internal.%s.svc.cluster.local", e2ePolarsClusterName, clusterNamespace)))
		})

		itRecreatesSchedulerOnTemplateChange(busyboxSpecConfig)
		itReplacesWorkersMarkedForDeletion(busyboxSpecConfig, 2)
		itScalesTheWorkerPool(busyboxSpecConfig, "up", 3)
		itScalesTheWorkerPool(busyboxSpecConfig, "down", 1)

		It("should reject a WorkerPool whose replicas falls outside [minReplicas, maxReplicas]", func() {
			By("attempting to create a PolarsCluster whose worker pool replicas is above maxReplicas")
			err := kubectlApply(polarsClusterManifestWithPoolBounds(clusterNamespace, "e2e-cluster-invalid-replicas", 1, 2, 5))
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("replicas must be within"))
		})

		It("should garbage-collect owned pods (scheduler and worker) when the PolarsCluster is deleted", func() {
			const clusterName = "e2e-cluster-gc"
			schedulerPod := schedulerPodNameForCluster(clusterName)

			By("creating a PolarsCluster with a worker pool of 1 desired replica")
			Expect(kubectlApply(polarsClusterManifest(clusterNamespace, clusterName, 1))).To(Succeed())

			var selector string
			By("waiting for its worker pod to be created")
			Eventually(func(g Gomega) {
				var err error
				selector, err = selectorForWorkerPool(clusterNamespace, clusterName)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(selector).NotTo(BeEmpty())

				names, err := podNamesForSelector(clusterNamespace, selector)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(names).To(HaveLen(1))
			}, 3*time.Minute, time.Second).Should(Succeed())

			By("waiting for its scheduler pod to be created")
			Eventually(func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "pod", schedulerPod, "-n", clusterNamespace)
				_, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
			}, 3*time.Minute, time.Second).Should(Succeed())

			By("deleting the PolarsCluster")
			cmd := exec.Command("kubectl", "delete", "polarscluster", clusterName, "-n", clusterNamespace)
			_, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())

			By("verifying its owned worker pod is garbage-collected")
			Eventually(func(g Gomega) {
				names, err := podNamesForSelector(clusterNamespace, selector)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(names).To(BeEmpty())
			}, 3*time.Minute, time.Second).Should(Succeed())

			By("verifying its owned scheduler pod is garbage-collected")
			Eventually(func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "pod", schedulerPod, "-n", clusterNamespace)
				_, err := utils.Run(cmd)
				g.Expect(err).To(HaveOccurred())
			}, 3*time.Minute, time.Second).Should(Succeed())
		})

		Context("Real polars-on-premises image", Label("real-image"), func() {
			BeforeAll(func() {
				By("loading the polars-on-premises dist image into Kind")
				err := utils.LoadImageToKindClusterWithName(realImageRepository + ":" + realImageTag)
				Expect(err).NotTo(HaveOccurred(), "Failed to load the dist image into Kind")

				By("creating a Secret from the on-prem enterprise license file")
				cmd := exec.Command("kubectl", "create", "secret", "generic", realImageLicenseSecret,
					"-n", clusterNamespace,
					fmt.Sprintf("--from-file=%s=%s", realImageLicenseSecretKey, realImageLicenseFile()))
				_, err = utils.Run(cmd)
				Expect(err).NotTo(HaveOccurred(),
					"Failed to create the license Secret (override the license path with POLARS_LICENSE_FILE)")
			})

			AfterAll(func() {
				By("deleting the real-image PolarsCluster")
				cmd := exec.Command("kubectl", "delete", "polarscluster", realImageClusterName,
					"-n", clusterNamespace, "--ignore-not-found")
				_, _ = utils.Run(cmd)

				By("deleting the license Secret")
				cmd = exec.Command("kubectl", "delete", "secret", realImageLicenseSecret,
					"-n", clusterNamespace, "--ignore-not-found")
				_, _ = utils.Run(cmd)
			})

			itReconcilesWorkerPool(realImageSpecConfig, realImagePolarsClusterManifest(clusterNamespace, realImageClusterName), 1)
			itCreatesAndReadiesSchedulerPod(realImageSpecConfig)
			itSetsOverallReadyCondition(realImageSpecConfig)
			itScalesTheWorkerPool(realImageSpecConfig, "up", 2)
			itReplacesWorkersMarkedForDeletion(realImageSpecConfig, 2)
			itScalesTheWorkerPool(realImageSpecConfig, "down", 1)
			itRecreatesSchedulerOnTemplateChange(realImageSpecConfig)
		})
	})
})

type sharedSpecConfig struct {
	namespace     string
	clusterName   string
	expectedImage string
	readyTimeout  time.Duration
	pollInterval  time.Duration
}

var busyboxSpecConfig = sharedSpecConfig{
	namespace:     clusterNamespace,
	clusterName:   e2ePolarsClusterName,
	expectedImage: "busybox:latest",
	readyTimeout:  1 * time.Minute,
	pollInterval:  time.Second,
}

var realImageSpecConfig = sharedSpecConfig{
	namespace:     clusterNamespace,
	clusterName:   realImageClusterName,
	expectedImage: realImageRuntimeImage,
	readyTimeout:  5 * time.Minute,
	pollInterval:  1 * time.Second,
}

func itReconcilesWorkerPool(cfg sharedSpecConfig, manifest string, replicas int) {
	It("should reconcile the WorkerPool to the desired replica count", func() {
		By(fmt.Sprintf("creating a PolarsCluster with a worker pool of %d desired replica(s)", replicas))
		Expect(kubectlApply(manifest)).To(Succeed())

		By("waiting for PolarsCluster.status.workerPool.selector to be populated")
		var selector string
		Eventually(func(g Gomega) {
			var err error
			selector, err = selectorForWorkerPool(cfg.namespace, cfg.clusterName)
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(selector).NotTo(BeEmpty())
		}).Should(Succeed())

		By(fmt.Sprintf("verifying %d pod(s) exist matching status.selector", replicas))
		Eventually(func(g Gomega) {
			names, err := podNamesForSelector(cfg.namespace, selector)
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(names).To(HaveLen(replicas))
		}).Should(Succeed())

		By("verifying a worker pod was built from the expected image, owned by the PolarsCluster")
		cmd := exec.Command("kubectl", "get", "pods", "-n", cfg.namespace,
			"-l", selector,
			"-o", "jsonpath={.items[0].spec.containers[0].image} {.items[0].metadata.ownerReferences[0].kind} {.items[0].metadata.ownerReferences[0].name}")
		output, err := utils.Run(cmd)
		Expect(err).NotTo(HaveOccurred())
		Expect(output).To(Equal(fmt.Sprintf("%s PolarsCluster %s", cfg.expectedImage, cfg.clusterName)))

		By("verifying PolarsCluster.status.workerPool reflects the ready replica count")
		Eventually(func(g Gomega) {
			cmd := exec.Command("kubectl", "get", "polarscluster", cfg.clusterName, "-n", cfg.namespace,
				"-o", "jsonpath={.status.workerPool.replicas} {.status.workerPool.readyReplicas}")
			output, err := utils.Run(cmd)
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(output).To(Equal(fmt.Sprintf("%d %d", replicas, replicas)))
		}, cfg.readyTimeout, cfg.pollInterval).Should(Succeed())
	})
}

func itCreatesAndReadiesSchedulerPod(cfg sharedSpecConfig) {
	It("should create and ready the cluster's singleton scheduler pod", func() {
		schedulerPod := schedulerPodNameForCluster(cfg.clusterName)

		By("verifying the scheduler pod exists, owned by the PolarsCluster")
		Eventually(func(g Gomega) {
			cmd := exec.Command("kubectl", "get", "pod", schedulerPod, "-n", cfg.namespace,
				"-o", "jsonpath={.spec.containers[0].image} {.metadata.ownerReferences[0].kind} {.metadata.ownerReferences[0].name}")
			output, err := utils.Run(cmd)
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(output).To(Equal(fmt.Sprintf("%s PolarsCluster %s", cfg.expectedImage, cfg.clusterName)))
		}).Should(Succeed())

		By("verifying the scheduler pod becomes Ready")
		Eventually(func(g Gomega) {
			cmd := exec.Command("kubectl", "get", "pod", schedulerPod, "-n", cfg.namespace,
				"-o", "jsonpath={.status.conditions[?(@.type=='Ready')].status}")
			output, err := utils.Run(cmd)
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(output).To(Equal("True"))
		}, cfg.readyTimeout, cfg.pollInterval).Should(Succeed())

		By("verifying PolarsCluster.status.scheduler.ready and the SchedulerReady condition")
		Eventually(func(g Gomega) {
			cmd := exec.Command("kubectl", "get", "polarscluster", cfg.clusterName, "-n", cfg.namespace,
				"-o", "jsonpath={.status.scheduler.ready} {.status.conditions[?(@.type=='SchedulerReady')].status}")
			output, err := utils.Run(cmd)
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(output).To(Equal("true True"))
		}, cfg.readyTimeout, cfg.pollInterval).Should(Succeed())
	})
}

func itSetsOverallReadyCondition(cfg sharedSpecConfig) {
	It("should set the overall Ready condition once the scheduler and worker pool are both ready", func() {
		Eventually(func(g Gomega) {
			cmd := exec.Command("kubectl", "get", "polarscluster", cfg.clusterName, "-n", cfg.namespace,
				"-o", "jsonpath={.status.conditions[?(@.type=='Ready')].status}")
			output, err := utils.Run(cmd)
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(output).To(Equal("True"))
		}, cfg.readyTimeout, cfg.pollInterval).Should(Succeed())
	})
}

func itScalesTheWorkerPool(cfg sharedSpecConfig, direction string, to int) {
	It(fmt.Sprintf("should scale the WorkerPool %s to %d ready worker(s)", direction, to), func() {
		By(fmt.Sprintf("patching PolarsCluster.spec.workerPool to set minReplicas/maxReplicas/replicas to %d", to))
		cmd := exec.Command("kubectl", "patch", "polarscluster", cfg.clusterName, "-n", cfg.namespace,
			"--type=merge",
			"-p", fmt.Sprintf(`{"spec":{"workerPool":{"minReplicas":%d,"maxReplicas":%d,"replicas":%d}}}`, to, to, to))
		_, err := utils.Run(cmd)
		Expect(err).NotTo(HaveOccurred())

		By(fmt.Sprintf("verifying %d pods exist matching status.selector", to))
		Eventually(func(g Gomega) {
			selector, err := selectorForWorkerPool(cfg.namespace, cfg.clusterName)
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(selector).NotTo(BeEmpty())

			names, err := podNamesForSelector(clusterNamespace, selector)
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(names).To(HaveLen(to))
		}, cfg.readyTimeout, cfg.pollInterval).Should(Succeed())

		By("verifying PolarsCluster.status.workerPool reflects the new ready replica count")
		Eventually(func(g Gomega) {
			cmd := exec.Command("kubectl", "get", "polarscluster", cfg.clusterName, "-n", cfg.namespace,
				"-o", "jsonpath={.status.workerPool.replicas} {.status.workerPool.readyReplicas}")
			output, err := utils.Run(cmd)
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(output).To(Equal(fmt.Sprintf("%d %d", to, to)))
		}, cfg.readyTimeout, cfg.pollInterval).Should(Succeed())

		By("verifying the cluster's overall Ready condition holds")
		Eventually(func(g Gomega) {
			cmd := exec.Command("kubectl", "get", "polarscluster", cfg.clusterName, "-n", cfg.namespace,
				"-o", "jsonpath={.status.conditions[?(@.type=='Ready')].status}")
			output, err := utils.Run(cmd)
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(output).To(Equal("True"))
		}, cfg.readyTimeout, cfg.pollInterval).Should(Succeed())
	})
}

func itReplacesWorkersMarkedForDeletion(cfg sharedSpecConfig, poolSize int) {
	It("should honor workersToDelete for targeted pod removal", func() {
		By("looking up the worker pool's pod selector")
		var selector string
		Eventually(func(g Gomega) {
			var err error
			selector, err = selectorForWorkerPool(cfg.namespace, cfg.clusterName)
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(selector).NotTo(BeEmpty())
		}).Should(Succeed())

		By("picking one of the pool's current pods to target for removal")
		names, err := podNamesForSelector(cfg.namespace, selector)
		Expect(err).NotTo(HaveOccurred())
		Expect(names).To(HaveLen(poolSize))
		victim := names[0]

		By("patching PolarsCluster.spec.workerPool.workersToDelete with that pod's name")
		cmd := exec.Command("kubectl", "patch", "polarscluster", cfg.clusterName, "-n", cfg.namespace,
			"--type=merge",
			"-p", fmt.Sprintf(`{"spec":{"workerPool":{"workersToDelete":["%s"]}}}`, victim))
		_, err = utils.Run(cmd)
		Expect(err).NotTo(HaveOccurred())

		By(fmt.Sprintf("verifying the named pod is deleted and replaced, keeping the pool at %d replicas", poolSize))
		Eventually(func(g Gomega) {
			names, err := podNamesForSelector(cfg.namespace, selector)
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(names).To(HaveLen(poolSize))
			g.Expect(names).NotTo(ContainElement(victim))
		}, cfg.readyTimeout, cfg.pollInterval).Should(Succeed())

		By("verifying the replacement worker becomes ready, restoring the pool's ready replica count")
		Eventually(func(g Gomega) {
			cmd := exec.Command("kubectl", "get", "polarscluster", cfg.clusterName, "-n", cfg.namespace,
				"-o", "jsonpath={.status.workerPool.replicas} {.status.workerPool.readyReplicas}")
			output, err := utils.Run(cmd)
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(output).To(Equal(fmt.Sprintf("%d %d", poolSize, poolSize)))
		}, cfg.readyTimeout, cfg.pollInterval).Should(Succeed())

		By("verifying the controller clears workersToDelete once processed")
		Eventually(func(g Gomega) {
			cmd := exec.Command("kubectl", "get", "polarscluster", cfg.clusterName, "-n", cfg.namespace,
				"-o", "jsonpath={.spec.workerPool.workersToDelete}")
			output, err := utils.Run(cmd)
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(output).To(BeEmpty())
		}).Should(Succeed())
	})
}

func itRecreatesSchedulerOnTemplateChange(cfg sharedSpecConfig) {
	It("should recreate the scheduler pod when its template changes", func() {
		schedulerPod := schedulerPodNameForCluster(cfg.clusterName)

		By("capturing the current scheduler pod UID")
		cmd := exec.Command("kubectl", "get", "pod", schedulerPod, "-n", cfg.namespace,
			"-o", "jsonpath={.metadata.uid}")
		oldUID, err := utils.Run(cmd)
		Expect(err).NotTo(HaveOccurred())
		Expect(oldUID).NotTo(BeEmpty())

		By("patching the scheduler pod template with a new env var")
		cmd = exec.Command("kubectl", "patch", "polarscluster", cfg.clusterName, "-n", cfg.namespace,
			"--type=json",
			"-p", `[{"op":"add","path":"/spec/schedulerSpec/podTemplate/spec/containers/0/env","value":[{"name":"ROLLOUT_MARKER","value":"1"}]}]`)
		_, err = utils.Run(cmd)
		Expect(err).NotTo(HaveOccurred())

		By("verifying a replacement scheduler pod comes up with the new env var")
		Eventually(func(g Gomega) {
			cmd := exec.Command("kubectl", "get", "pod", schedulerPod, "-n", cfg.namespace,
				"-o", "jsonpath={.metadata.uid}")
			newUID, err := utils.Run(cmd)
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(newUID).NotTo(Equal(oldUID))

			marker, err := containerEnvValue(cfg.namespace, schedulerPod, "ROLLOUT_MARKER")
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(marker).To(Equal("1"))
		}, cfg.readyTimeout, cfg.pollInterval).Should(Succeed())

		By("verifying the replacement scheduler becomes ready and the cluster returns to Ready")
		Eventually(func(g Gomega) {
			cmd := exec.Command("kubectl", "get", "polarscluster", cfg.clusterName, "-n", cfg.namespace,
				"-o", "jsonpath={.status.scheduler.ready} {.status.conditions[?(@.type=='Ready')].status}")
			output, err := utils.Run(cmd)
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(output).To(Equal("true True"))
		}, cfg.readyTimeout, cfg.pollInterval).Should(Succeed())
	})
}

const e2ePolarsClusterName = "e2e-cluster"

const realImageClusterName = "e2e-real-image-cluster"

const realImageRepository = "polars-on-premises"
const realImageTag = "latest"

const realImageRuntimeImage = "python:3.13.9-slim-bookworm"

const realImageLicenseSecret = "e2e-real-image-license"
const realImageLicenseSecretKey = "license.json"

const clusterLicenseYAML = `
  license:
    onPrem:
      clientID:
        valueFrom:
          fieldRef: { fieldPath: metadata.name }
      clientSecret:
        valueFrom:
          fieldRef: { fieldPath: metadata.name }
      workspaceID:
        valueFrom:
          fieldRef: { fieldPath: metadata.name }`

const schedulerSpecYAML = `
  schedulerSpec:
    podTemplate:
      spec:
        containers:
          - name: scheduler
            image: busybox:latest
            command: ["sleep", "3600"]
            readinessProbe:
              exec:
                command: ["/bin/true"]
            resources:
              requests:
                ephemeral-storage: 10Gi
            securityContext:
              allowPrivilegeEscalation: false
              capabilities:
                drop: ["ALL"]
              runAsNonRoot: true
              runAsUser: 1000
              seccompProfile:
                type: RuntimeDefault`

const workerPoolPodTemplateYAML = `
      spec:
        containers:
          - name: worker
            image: busybox:latest
            command: ["sleep", "3600"]
            readinessProbe:
              exec:
                command: ["/bin/true"]
            resources:
              requests:
                ephemeral-storage: 10Gi
            securityContext:
              allowPrivilegeEscalation: false
              capabilities:
                drop: ["ALL"]
              runAsNonRoot: true
              runAsUser: 1000
              seccompProfile:
                type: RuntimeDefault`

const storageLocationsYAML = `
  anonymousResults:
    s3:
      endpoint: "s3://polars-e2e-anonymous-results/results"
  checkpointData:
    s3:
      endpoint: "s3://polars-e2e-checkpoint-data/checkpoints"`

const anonymousResultsS3Endpoint = "s3://polars-e2e-anonymous-results/results"
const checkpointDataS3Endpoint = "s3://polars-e2e-checkpoint-data/checkpoints"

func polarsClusterManifest(ns, name string, poolReplicas int) string {
	return fmt.Sprintf(`
apiVersion: compute.pola.rs/v1
kind: PolarsCluster
metadata:
  name: %s
  namespace: %s
spec:
%s
%s
%s
  workerPool:
    podTemplate:
%s
    minReplicas: %d
    maxReplicas: %d
    replicas: %d
`, name, ns, clusterLicenseYAML, schedulerSpecYAML, storageLocationsYAML, workerPoolPodTemplateYAML, poolReplicas, poolReplicas, poolReplicas)
}

func polarsClusterManifestWithPoolBounds(ns, name string, minReplicas, maxReplicas, replicas int) string {
	return fmt.Sprintf(`
apiVersion: compute.pola.rs/v1
kind: PolarsCluster
metadata:
  name: %s
  namespace: %s
spec:
%s
%s
  workerPool:
    podTemplate:
%s
    minReplicas: %d
    maxReplicas: %d
    replicas: %d
`, name, ns, clusterLicenseYAML, schedulerSpecYAML, workerPoolPodTemplateYAML, minReplicas, maxReplicas, replicas)
}

func realImagePolarsClusterManifest(ns, name string) string {
	return fmt.Sprintf(`
apiVersion: compute.pola.rs/v1
kind: PolarsCluster
metadata:
  name: %s
  namespace: %s
spec:
  acceptEula: true
  license:
    onPremEnterprise:
      secretName: %s
      secretProperty: %s
  runtime:
    composed:
      dist:
        repository: %s
        tag: %q
        pullPolicy: Never
  schedulerSpec:
    podTemplate:
      spec:
        containers:
          - name: scheduler
  workerPool:
    minReplicas: 1
    maxReplicas: 1
    replicas: 1
`, name, ns, realImageLicenseSecret, realImageLicenseSecretKey, realImageRepository, realImageTag)
}

func realImageLicenseFile() string {
	if path := os.Getenv("POLARS_LICENSE_FILE"); path != "" {
		return path
	}
	home, err := os.UserHomeDir()
	Expect(err).NotTo(HaveOccurred(), "Failed to resolve the home directory for the default license path")
	return filepath.Join(home, ".cache", "polars-cloud", "license", "license.json")
}

func selectorForWorkerPool(ns, clusterName string) (string, error) {
	cmd := exec.Command("kubectl", "get", "polarscluster", clusterName, "-n", ns,
		"-o", "jsonpath={.status.workerPool.selector}")
	return utils.Run(cmd)
}

func schedulerPodNameForCluster(clusterName string) string {
	return clusterName + "-scheduler"
}

func containerEnvValue(ns, podName, envName string) (string, error) {
	cmd := exec.Command("kubectl", "get", "pod", podName, "-n", ns,
		"-o", fmt.Sprintf("jsonpath={.spec.containers[0].env[?(@.name=='%s')].value}", envName))
	return utils.Run(cmd)
}

func kubectlApply(yaml string) error {
	tmpFile, err := os.CreateTemp("", "e2e-manifest-*.yaml")
	if err != nil {
		return err
	}
	defer os.Remove(tmpFile.Name())

	if _, err := tmpFile.WriteString(yaml); err != nil {
		return err
	}
	if err := tmpFile.Close(); err != nil {
		return err
	}

	cmd := exec.Command("kubectl", "apply", "-f", tmpFile.Name())
	_, err = utils.Run(cmd)
	return err
}

func podNamesForSelector(ns, selector string) ([]string, error) {
	cmd := exec.Command("kubectl", "get", "pods", "-n", ns, "-l", selector,
		"-o", "jsonpath={.items[*].metadata.name}")
	output, err := utils.Run(cmd)
	if err != nil {
		return nil, err
	}
	return strings.Fields(output), nil
}

// serviceAccountToken returns a token for the specified service account in the given namespace.
// It uses the Kubernetes TokenRequest API to generate a token by directly sending a request
// and parsing the resulting token from the API response.
func serviceAccountToken() (string, error) {
	const tokenRequestRawString = `{
		"apiVersion": "authentication.k8s.io/v1",
		"kind": "TokenRequest"
	}`

	By("creating temporary file to store the token request")
	secretName := fmt.Sprintf("%s-token-request", serviceAccountName)
	tokenRequestFile := filepath.Join("/tmp", secretName)
	err := os.WriteFile(tokenRequestFile, []byte(tokenRequestRawString), os.FileMode(0o644))
	if err != nil {
		return "", err
	}

	var out string
	verifyTokenCreation := func(g Gomega) {
		By("executing kubectl command to create the token")
		cmd := exec.Command("kubectl", "create", "--raw", fmt.Sprintf(
			"/api/v1/namespaces/%s/serviceaccounts/%s/token",
			namespace,
			serviceAccountName,
		), "-f", tokenRequestFile)

		output, err := cmd.CombinedOutput()
		g.Expect(err).NotTo(HaveOccurred())

		By("parsing the JSON output to extract the token")
		var token tokenRequest
		err = json.Unmarshal(output, &token)
		g.Expect(err).NotTo(HaveOccurred())

		out = token.Status.Token
	}
	Eventually(verifyTokenCreation).Should(Succeed())

	return out, err
}

// getMetricsOutput retrieves and returns the logs from the curl pod used to access the metrics endpoint.
func getMetricsOutput() (string, error) {
	By("getting the curl-metrics logs")
	cmd := exec.Command("kubectl", "logs", "curl-metrics", "-n", namespace)
	return utils.Run(cmd)
}

// tokenRequest is a simplified representation of the Kubernetes TokenRequest API response,
// containing only the token field that we need to extract.
type tokenRequest struct {
	Status struct {
		Token string `json:"token"`
	} `json:"status"`
}
