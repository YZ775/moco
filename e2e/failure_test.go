package e2e

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	mocov1beta2 "github.com/cybozu-go/moco/api/v1beta2"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
)

//go:embed testdata/failure.yaml
var failureYAML string

var _ = Context("failure", func() {
	if doUpgrade {
		return
	}

	It("should construct a 3-instance cluster", func() {
		kubectlSafe(fillTemplate(failureYAML), "apply", "-f", "-")
		Eventually(func() error {
			cluster, err := getCluster("failure", "test")
			if err != nil {
				return err
			}
			for _, cond := range cluster.Status.Conditions {
				if cond.Type != mocov1beta2.ConditionHealthy {
					continue
				}
				if cond.Status == corev1.ConditionTrue {
					return nil
				}
				return fmt.Errorf("cluster is not healthy: %s", cond.Status)
			}
			return errors.New("no health condition")
		}).Should(Succeed())

		kubectlSafe(nil, "moco", "-n", "failure", "mysql", "-u", "moco-writable", "test", "--",
			"-e", "CREATE DATABASE test")
		kubectlSafe(nil, "moco", "-n", "failure", "mysql", "-u", "moco-writable", "test", "--",
			"-D", "test", "-e", "CREATE TABLE t (x char(32)) ENGINE=InnoDB")
	})

	It("should make a new replica pod ready", func() {
		By("keeping writing data to the cluster")
		ctx, cancel := context.WithCancel(context.Background())
		var wg sync.WaitGroup
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-ctx.Done():
					return
				default:
					kubectlSafe(nil, "moco", "-n", "failure", "mysql", "-u", "moco-writable", "test", "--",
						"-D", "test", "-e", "INSERT INTO t VALUE ('foo'); COMMIT")
				}
			}
		}()

		By("deleting a replica pod")
		kubectlSafe(nil, "delete", "-n", "failure", "pod", "moco-test-2")

		// Wait for the controller to update the cluster status.
		// This wait should be sufficiently larger than (check interval[5s] + time to retry in the ClusterManager[3s * 3times]).
		time.Sleep(30 * time.Second)

		By("waiting the cluster becomes ready")
		Eventually(func() error {
			cluster, err := getCluster("failure", "test")
			if err != nil {
				return err
			}
			for _, cond := range cluster.Status.Conditions {
				if cond.Type != mocov1beta2.ConditionHealthy {
					continue
				}
				if cond.Status == corev1.ConditionTrue {
					return nil
				}
				return fmt.Errorf("cluster is not healthy: %s", cond.Status)
			}
			return errors.New("no health condition")
		}).Should(Succeed())

		cancel()
		wg.Wait()
	})

	It("should delete clusters", func() {
		kubectlSafe(nil, "delete", "-n", "failure", "mysqlclusters", "--all")

		Eventually(func() error {
			out, err := kubectl(nil, "get", "-n", "failure", "pod", "-o", "json")
			if err != nil {
				return err
			}
			pods := &corev1.PodList{}
			if err := json.Unmarshal(out, pods); err != nil {
				return err
			}
			if len(pods.Items) > 0 {
				return errors.New("wait until all Pods are deleted")
			}
			return nil
		}).Should(Succeed())
	})
})
