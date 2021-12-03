// Unless explicitly stated otherwise all files in this repository are licensed
// under the Apache License Version 2.0.
// This product includes software developed at Datadog (https://www.datadoghq.com/).
// Copyright 2016-present Datadog, Inc.

// +build kubeapiserver

package apiserver

import (
	"context"
	"fmt"
	"time"

	"golang.org/x/sync/errgroup"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/cache"

	"github.com/DataDog/datadog-agent/pkg/config"
	"github.com/DataDog/datadog-agent/pkg/util/log"
	"github.com/DataDog/datadog-agent/pkg/util/retry"
	"github.com/DataDog/watermarkpodautoscaler/api/v1alpha1"
)

// SyncInformers should be called after the instantiation of new informers.
// It's blocking until the informers are synced or the timeout exceeded.
func SyncInformers(informers map[InformerName]cache.SharedInformer) error {
	var g errgroup.Group
	// syncTimeout can be used to wait for the kubernetes client-go cache to sync.
	// It cannot be retrieved at the package-level due to the package being imported before configs are loaded.
	syncTimeout := config.Datadog.GetDuration("kube_cache_sync_timeout_seconds") * time.Second
	for name := range informers {
		name := name // https://golang.org/doc/faq#closures_and_goroutines
		config := retry.Config{
			Name:              string(name),
			Strategy:          retry.Backoff,
			InitialRetryDelay: syncTimeout,
			MaxRetryDelay:     syncTimeout + time.Duration(60)*time.Second,
		}
		g.Go(func() error {
			nextTry := config.InitialRetryDelay
			lastTry := false
			for {
				ctx, cancel := context.WithTimeout(context.Background(), nextTry)
				defer cancel()
				start := time.Now()
				if !cache.WaitForCacheSync(ctx.Done(), informers[name].HasSynced) {
					log.Warnf("couldn't sync informer %s in %v", name, time.Now().Sub(start))
					nextTry = nextTry + (1<<config.RetryCount)*time.Second
					config.RetryCount++
					log.Warnf("increase kube_cache_sync_timeout_seconds to %s", nextTry)
					if lastTry {
						return fmt.Errorf("couldn't sync informer %s in %v", name, time.Now().Sub(start))
					}
					if nextTry >= config.MaxRetryDelay {
						nextTry = config.MaxRetryDelay
						lastTry = true
					}
				} else {
					log.Debugf("Sync done for informer %s in %v, last resource version: %s", name, time.Now().Sub(start), informers[name].LastSyncResourceVersion())
					return nil
				}
			}
		})
	}
	return g.Wait()
}

func UnstructuredIntoWPA(obj interface{}, structDest *v1alpha1.WatermarkPodAutoscaler) error {
	unstrObj, ok := obj.(*unstructured.Unstructured)
	if !ok {
		return fmt.Errorf("could not cast Unstructured object: %v", obj)
	}
	return runtime.DefaultUnstructuredConverter.FromUnstructured(unstrObj.UnstructuredContent(), structDest)
}

func UnstructuredFromWPA(structIn *v1alpha1.WatermarkPodAutoscaler, unstructOut *unstructured.Unstructured) error {
	content, err := runtime.DefaultUnstructuredConverter.ToUnstructured(structIn)
	if err != nil {
		return fmt.Errorf("Unable to convert WatermarkPodAutoscaler %v: %w", structIn, err)
	}
	unstructOut.SetUnstructuredContent(content)
	return nil
}
