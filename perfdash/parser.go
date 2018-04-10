/*
Copyright 2016 The Kubernetes Authors.

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
	"encoding/json"
	"fmt"
	"k8s.io/kubernetes/test/e2e/framework/metrics"
	"k8s.io/kubernetes/test/e2e/perftype"
	"math"
	"os"
	"strconv"
)

func stripCount(data *perftype.DataItem) {
	delete(data.Labels, "Count")
}

func createRequestCountData(data *perftype.DataItem) error {
	data.Unit = ""
	data.Data = make(map[string]float64)
	requestCountString, ok := data.Labels["Count"]
	if !ok {
		return fmt.Errorf("no 'Count' label")
	}
	requestCount, err := strconv.ParseFloat(requestCountString, 64)
	if err != nil {
		return fmt.Errorf("couldn't parse count: %v", err)
	}
	data.Data["RequestCount"] = requestCount
	return nil
}

func parseResponsivenessData(data []byte, buildNumber int, testResult *BuildData) {
	build := fmt.Sprintf("%d", buildNumber)
	obj := perftype.PerfData{}
	if err := json.Unmarshal(data, &obj); err != nil {
		fmt.Fprintf(os.Stderr, "error parsing JSON in build %d: %v %s\n", buildNumber, err, string(data))
		return
	}
	if testResult.Version == "" {
		testResult.Version = obj.Version
	}
	if testResult.Version == obj.Version {
		for i := range obj.DataItems {
			stripCount(&obj.DataItems[i])
			testResult.Builds[build] = append(testResult.Builds[build], obj.DataItems[i])
		}
	}
}

type resourceUsagePercentiles map[string][]resourceUsages

type resourceUsages struct {
	Name   string  `json:"Name"`
	Cpu    float64 `json:"Cpu"`
	Memory int     `json:"Mem"`
}

type resourceUsage struct {
	Cpu    float64
	Memory float64
}
type usageAtPercentiles map[string]resourceUsage
type podNameToUsage map[string]usageAtPercentiles

func parseResourceUsageData(data []byte, buildNumber int, testResult *BuildData) {
	testResult.Version = "v1"
	build := fmt.Sprintf("%d", buildNumber)
	var obj resourceUsagePercentiles
	if err := json.Unmarshal(data, &obj); err != nil {
		fmt.Fprintf(os.Stderr, "error parsing JSON in build %d: %v %s\n", buildNumber, err, string(data))
		return
	}
	usage := make(podNameToUsage)
	for percentile, items := range obj {
		for _, item := range items {
			name := RemoveDisambiguationInfixes(item.Name)
			if _, ok := usage[name]; !ok {
				usage[name] = make(usageAtPercentiles)
			}
			cpu, memory := float64(item.Cpu), float64(item.Memory)
			if otherUsage, ok := usage[name][percentile]; ok {
				// Note that we take max of each resource separately, potentially manufacturing a
				// "franken-sample" which was never seen in the wild. We do this hoping that such result
				// will be more stable across runs.
				cpu = math.Max(cpu, otherUsage.Cpu)
				memory = math.Max(memory, otherUsage.Memory)
			}
			usage[name][percentile] = resourceUsage{cpu, memory}
		}
	}
	for podName, usageAtPercentiles := range usage {
		cpu := perftype.DataItem{Unit: "cores", Labels: map[string]string{"PodName": podName, "Resource": "CPU"}, Data: make(map[string]float64)}
		memory := perftype.DataItem{Unit: "MiB", Labels: map[string]string{"PodName": podName, "Resource": "memory"}, Data: make(map[string]float64)}
		for percentile, usage := range usageAtPercentiles {
			cpu.Data[percentile] = usage.Cpu
			memory.Data[percentile] = usage.Memory / (1024 * 1024)
		}
		testResult.Builds[build] = append(testResult.Builds[build], cpu)
		testResult.Builds[build] = append(testResult.Builds[build], memory)
	}
}

func parseRequestCountData(data []byte, buildNumber int, testResult *BuildData) {
	build := fmt.Sprintf("%d", buildNumber)
	obj := perftype.PerfData{}
	if err := json.Unmarshal(data, &obj); err != nil {
		fmt.Fprintf(os.Stderr, "error parsing JSON in build %d: %v %s\n", buildNumber, err, string(data))
		return
	}
	if testResult.Version == "" {
		testResult.Version = obj.Version
	}
	if testResult.Version == obj.Version {
		for i := range obj.DataItems {
			if err := createRequestCountData(&obj.DataItems[i]); err != nil {
				fmt.Fprintf(os.Stderr, "error creating request count data in build %d dataItem %d: %v\n", buildNumber, i, err)
				continue
			}
			stripCount(&obj.DataItems[i])
			testResult.Builds[build] = append(testResult.Builds[build], obj.DataItems[i])
		}
	}
}

func parseApiserverRequestCount(data []byte, buildNumber int, testResult *BuildData) {
	testResult.Version = "v1"
	build := fmt.Sprintf("%d", buildNumber)
	var obj metrics.MetricsCollection
	if err := json.Unmarshal(data, &obj); err != nil {
		fmt.Fprintf(os.Stderr, "error parsing JSON in build %d: %v %s\n", buildNumber, err, string(data))
		return
	}
	if obj.ApiServerMetrics == nil {
		fmt.Fprintf(os.Stderr, "no ApiServerMetrics data in build %d", buildNumber)
		return
	}
	metric, ok := obj.ApiServerMetrics["apiserver_request_count"]
	if !ok {
		fmt.Fprintf(os.Stderr, "no apiserver_request_count metric data in build %d", buildNumber)
		return
	}
	for i := range metric {
		perfData := perftype.DataItem{Unit: "", Data: make(map[string]float64), Labels: make(map[string]string)}
		for k, v := range metric[i].Metric {
			perfData.Labels[string(k)] = string(v)
		}
		delete(perfData.Labels, "__name__")
		delete(perfData.Labels, "contentType")
		perfData.Data["RequestCount"] = float64(metric[i].Value)
		testResult.Builds[build] = append(testResult.Builds[build], perfData)
	}
}
