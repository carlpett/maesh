package try

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/cenkalti/backoff/v3"
	"github.com/containous/maesh/internal/k8s"
	"github.com/containous/traefik/v2/pkg/safe"
	log "github.com/sirupsen/logrus"
	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/client-go/util/retry"
)

const (
	// CITimeoutMultiplier is the multiplier for all timeout in the CI
	CITimeoutMultiplier = 3
)

type Try struct {
	client *k8s.ClientWrapper
}

func NewTry(client *k8s.ClientWrapper) *Try {
	return &Try{client: client}
}

// WaitReadyDeployment wait until the deployment is ready.
func (t *Try) WaitReadyDeployment(name string, namespace string, timeout time.Duration) error {
	ebo := backoff.NewExponentialBackOff()
	ebo.MaxElapsedTime = applyCIMultiplier(timeout)

	if err := backoff.Retry(safe.OperationWithRecover(func() error {
		d, exists, err := t.client.GetDeployment(namespace, name)
		if err != nil {
			return fmt.Errorf("unable get the deployment %q in namespace %q: %v", name, namespace, err)
		}
		if !exists {
			return fmt.Errorf("deployment %q has not been yet created", name)
		}
		if d.Status.Replicas == 0 {
			return fmt.Errorf("deployment %q has no replicas", name)
		}

		if d.Status.ReadyReplicas == d.Status.Replicas {
			return nil
		}
		return errors.New("deployment not ready")
	}), ebo); err != nil {
		return fmt.Errorf("unable get the deployment %q in namespace %q: %v", name, namespace, err)
	}

	return nil
}

// WaitUpdateDeployment waits until the deployment is successfully updated and ready.
func (t *Try) WaitUpdateDeployment(deployment *appsv1.Deployment, timeout time.Duration) error {
	retryErr := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		_, err := t.client.UpdateDeployment(deployment)
		return err
	})

	if retryErr != nil {
		return fmt.Errorf("unable to update deployment %q: %v", deployment.Name, retryErr)
	}

	return t.WaitReadyDeployment(deployment.Name, deployment.Namespace, timeout)
}

// WaitDeleteDeployment wait until the deployment is delete.
func (t *Try) WaitDeleteDeployment(name string, namespace string, timeout time.Duration) error {
	ebo := backoff.NewExponentialBackOff()
	ebo.MaxElapsedTime = applyCIMultiplier(timeout)

	if err := backoff.Retry(safe.OperationWithRecover(func() error {
		_, exists, err := t.client.GetDeployment(namespace, name)
		if err != nil {
			return fmt.Errorf("unable get the deployment %q in namespace %q: %v", name, namespace, err)
		}
		if exists {
			return fmt.Errorf("deployment %q exist", name)
		}

		return nil
	}), ebo); err != nil {
		return fmt.Errorf("unable get the deployment %q in namespace %q: %v", name, namespace, err)
	}

	return nil
}

// WaitCommandExecute wait until the command is executed.
func (t *Try) WaitCommandExecute(command string, argSlice []string, expected string, timeout time.Duration) error {
	ebo := backoff.NewExponentialBackOff()
	ebo.MaxElapsedTime = applyCIMultiplier(timeout)

	var output []byte
	if err := backoff.Retry(safe.OperationWithRecover(func() error {
		cmd := exec.Command(command, argSlice...)
		cmd.Env = os.Environ()
		var errOpt error
		output, errOpt = cmd.CombinedOutput()
		if errOpt != nil {
			return fmt.Errorf("unable execute command %s %s - output %s: \n%v", command, strings.Join(argSlice, " "), output, errOpt)
		}

		if !strings.Contains(string(output), expected) {
			return fmt.Errorf("output %s does not contain %s", string(output), expected)
		}

		return nil
	}), ebo); err != nil {
		return fmt.Errorf("unable execute command %s %s: \n%v", command, strings.Join(argSlice, " "), err)
	}

	return nil
}

// WaitCommandExecuteReturn wait until the command is executed.
func (t *Try) WaitCommandExecuteReturn(command string, argSlice []string, timeout time.Duration) (string, error) {
	ebo := backoff.NewExponentialBackOff()
	ebo.MaxElapsedTime = applyCIMultiplier(timeout)

	var output []byte
	if err := backoff.Retry(safe.OperationWithRecover(func() error {
		cmd := exec.Command(command, argSlice...)
		cmd.Env = os.Environ()
		var errOpt error
		output, errOpt = cmd.CombinedOutput()
		if errOpt != nil {
			return fmt.Errorf("unable execute command %s %s - output %s: \n%v", command, strings.Join(argSlice, " "), output, errOpt)
		}

		return nil
	}), ebo); err != nil {
		return "", fmt.Errorf("unable execute command %s %s: \n%v", command, strings.Join(argSlice, " "), err)
	}

	return string(output), nil
}

// WaitFunction wait until the command is executed.
func (t *Try) WaitFunction(f func() error, timeout time.Duration) error {
	ebo := backoff.NewExponentialBackOff()
	ebo.MaxElapsedTime = applyCIMultiplier(timeout)

	if err := backoff.Retry(safe.OperationWithRecover(f), ebo); err != nil {
		return fmt.Errorf("unable execute function: %v", err)
	}

	return nil
}

// WaitDeleteNamespace wait until the namespace is delete.
func (t *Try) WaitDeleteNamespace(name string, timeout time.Duration) error {
	ebo := backoff.NewExponentialBackOff()
	ebo.MaxElapsedTime = applyCIMultiplier(timeout)

	if err := backoff.Retry(safe.OperationWithRecover(func() error {
		_, exists, err := t.client.GetNamespace(name)
		if err != nil {
			return fmt.Errorf("unable get the namesapce %q: %v", name, err)
		}
		if exists {
			return fmt.Errorf("namesapce %q exist", name)
		}

		return nil
	}), ebo); err != nil {
		return fmt.Errorf("unable get the namesapce %q: %v", name, err)
	}

	return nil
}

// WaitClientCreated wait until the file is created.
func (t *Try) WaitClientCreated(url string, kubeConfigPath string, timeout time.Duration) (*k8s.ClientWrapper, error) {
	ebo := backoff.NewExponentialBackOff()
	ebo.MaxElapsedTime = applyCIMultiplier(timeout)

	var clients *k8s.ClientWrapper
	var err error
	if err = backoff.Retry(safe.OperationWithRecover(func() error {
		clients, err = k8s.NewClientWrapper(url, kubeConfigPath)
		if err != nil {
			return fmt.Errorf("unable to create clients: %v", err)
		}

		if _, err = clients.KubeClient.ServerVersion(); err != nil {
			return fmt.Errorf("unable to get server version: %v", err)
		}

		return nil
	}), ebo); err != nil {
		return nil, fmt.Errorf("unable to create clients: %v", err)
	}

	return clients, nil
}

func applyCIMultiplier(timeout time.Duration) time.Duration {
	if os.Getenv("CI") == "" {
		return timeout
	}

	ciTimeoutMultiplier := getCITimeoutMultiplier()
	log.Debug("Apply CI multiplier:", ciTimeoutMultiplier)
	return time.Duration(float64(timeout) * ciTimeoutMultiplier)

}

func getCITimeoutMultiplier() float64 {
	ciTimeoutMultiplier := os.Getenv("CI_TIMEOUT_MULTIPLIER")
	if ciTimeoutMultiplier == "" {
		return CITimeoutMultiplier
	}

	multiplier, err := strconv.ParseFloat(ciTimeoutMultiplier, 64)
	if err != nil {
		return CITimeoutMultiplier
	}

	return multiplier
}
