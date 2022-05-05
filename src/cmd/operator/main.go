/*
Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"

	"github.com/Dynatrace/dynatrace-operator/src/kubesystem"
	"github.com/Dynatrace/dynatrace-operator/src/logger"
	"github.com/Dynatrace/dynatrace-operator/src/version"
	"github.com/spf13/pflag"
	corev1 "k8s.io/api/core/v1"
	_ "k8s.io/client-go/plugin/pkg/client/auth/gcp"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/config"
	"sigs.k8s.io/controller-runtime/pkg/cluster"
	"sigs.k8s.io/controller-runtime/pkg/manager"
)

var (
	log = logger.NewDTLogger().WithName("main")
)

const (
	operatorCmd         = "operator"
	csiDriverCmd        = "csi-driver"
	standaloneCmd       = "init"
	webhookServerCmd    = "webhook-server"
	troubleshootHttpCmd = "troubleshoot-http"
	troubleshootCmd     = "troubleshoot"
)

var (
	httpMethod         string
	httpUrl            string
	httpAuthMethod     string
	httpAuthToken      string
	dynakubeSecretName string
	dynatraceNamespace string
)

func httpFlags() *pflag.FlagSet {
	httpFlagSet := pflag.NewFlagSet("http", pflag.ExitOnError)
	httpFlagSet.StringVar(&httpMethod, "http-method", "", "HTTP method: GET, POST, HEAD.")
	httpFlagSet.StringVar(&httpUrl, "http-url", "", "HTTP URL.")
	httpFlagSet.StringVar(&httpAuthMethod, "http-auth-method", "", "HTTP authentication method: Api-Token, Basic.")
	httpFlagSet.StringVar(&httpAuthToken, "http-auth-token", "", "HTTP authentication token.")

	httpFlagSet.StringVar(&dynakubeSecretName, "dynakube-secret-name", "", "Dynakube secret name.")
	httpFlagSet.StringVar(&dynatraceNamespace, "namespace", "dynatrace", "namespace.")
	return httpFlagSet
}

var errBadSubcmd = fmt.Errorf("subcommand must be %s, %s, %s or %s", operatorCmd, csiDriverCmd, webhookServerCmd, standaloneCmd)

func main() {
	pflag.CommandLine.AddFlagSet(webhookServerFlags())
	pflag.CommandLine.AddFlagSet(csiDriverFlags())
	pflag.CommandLine.AddFlagSet(httpFlags())
	pflag.Parse()

	ctrl.SetLogger(log)

	version.LogVersion()

	namespace := os.Getenv("POD_NAMESPACE")
	var mgr manager.Manager
	var err error
	var cleanUp func()

	subCmd := getSubCommand()
	switch subCmd {
	case operatorCmd:
		cfg := getKubeConfig()
		if !kubesystem.DeployedViaOLM() {
			// setup manager only for certificates
			bootstrapperCtx, done := context.WithCancel(context.TODO())
			mgr, err := setupBootstrapper(namespace, cfg, done)
			exitOnError(err, "bootstrapper setup failed")
			exitOnError(mgr.Start(bootstrapperCtx), "problem running bootstrap manager")
		}
		// bootstrap manager stopped, starting full manager
		mgr, err = setupOperator(namespace, cfg)
		exitOnError(err, "operator setup failed")
	case csiDriverCmd:
		cfg := getKubeConfig()
		mgr, cleanUp, err = setupCSIDriver(namespace, cfg)
		exitOnError(err, "csi driver setup failed")
		defer cleanUp()
	case webhookServerCmd:
		cfg := getKubeConfig()
		mgr, cleanUp, err = setupWebhookServer(namespace, cfg)
		exitOnError(err, "webhook-server setup failed")
		defer cleanUp()
	case standaloneCmd:
		err := startStandAloneInit()
		exitOnError(err, "initContainer command failed")
		os.Exit(0)
	case troubleshootHttpCmd:
		os.Exit(troubleshootHttp(httpMethod, httpUrl, httpAuthMethod, httpAuthToken))
	case troubleshootCmd:
		cfg := getKubeConfig()
		os.Exit(troubleshoot(cfg, dynatraceNamespace, dynakubeSecretName))
	default:
		log.Error(errBadSubcmd, "unknown subcommand", "command", subCmd)
		os.Exit(1)
	}

	signalHandler := ctrl.SetupSignalHandler()
	log.Info("starting manager")
	exitOnError(mgr.Start(signalHandler), "problem running manager")
}

func getKubeConfig() *rest.Config {
	cfg, err := config.GetConfig()
	if err != nil {
		log.Error(err, "error getting kube config")
		os.Exit(1)
	}
	return cfg
}

func exitOnError(err error, msg string) {
	if err != nil {
		log.Error(err, msg)
		os.Exit(1)
	}
}

func getSubCommand() string {
	if args := pflag.Args(); len(args) > 0 {
		return args[0]
	}
	return operatorCmd
}

func troubleshootHttp(httpMethod string, httpUrl string, authMethod string, authToken string) int {
	params := url.Values{}
	body := strings.NewReader(params.Encode())

	req, err := http.NewRequest(httpMethod, httpUrl, body)
	if err != nil {
		log.Error(err, "troubleshoot")
		return 1
	}
	if authMethod != "" && authToken != "" {
		req.Header.Set("Authorization", authMethod+" "+authToken)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		log.Error(err, "troubleshoot")
		return 1
	}
	defer resp.Body.Close()

	b, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Error(err, "troubleshoot")
		return 1
	}

	log.Info("troubleshoot", "status", resp.StatusCode, "body", string(b))
	return 0
}

func troubleshoot(cfg *rest.Config, namespace string, dynakubeSecretName string) int {
	k8scluster, err := cluster.New(cfg)
	if err != nil {
		log.Error(err, "troubleshoot")
		os.Exit(1)
	}
	apiReader := k8scluster.GetAPIReader()
	var dynakubeSecret corev1.Secret
	if err := apiReader.Get(context.TODO(), client.ObjectKey{Name: dynakubeSecretName, Namespace: namespace}, &dynakubeSecret); err != nil {
		log.Error(err, "troubleshoot")
		os.Exit(1)
	}
	log.Info("troubleshoot", "secret", "found")
	return 0
}
