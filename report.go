// Copyright 2019 Google Inc. All Rights Reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package main

import (
	"context"
	"encoding/csv"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"text/template"

	"github.com/google/go-licenses/licenses"
	"github.com/spf13/cobra"
	"k8s.io/klog/v2"
)

const (
	UNKNOWN = "Unknown"
)

var (
	reportHelp = "Prints report of all licenses that apply to one or more Go packages and their dependencies."
	reportCmd  = &cobra.Command{
		Use:   "report <package> [package...]",
		Short: reportHelp,
		Long:  reportHelp + packageHelp,
		Args:  cobra.MinimumNArgs(1),
		RunE:  reportMain,
	}

	templateFile string
)

func init() {
	reportCmd.Flags().StringVar(&templateFile, "template", "", "Custom Go template file to use for report")

	rootCmd.AddCommand(reportCmd)
}

var bsd3clause = `Copyright (c) 2009 The Go Authors. All rights reserved.

Redistribution and use in source and binary forms, with or without
modification, are permitted provided that the following conditions are
met:

   * Redistributions of source code must retain the above copyright
notice, this list of conditions and the following disclaimer.
   * Redistributions in binary form must reproduce the above
copyright notice, this list of conditions and the following disclaimer
in the documentation and/or other materials provided with the
distribution.
   * Neither the name of Google Inc. nor the names of its
contributors may be used to endorse or promote products derived from
this software without specific prior written permission.

THIS SOFTWARE IS PROVIDED BY THE COPYRIGHT HOLDERS AND CONTRIBUTORS
"AS IS" AND ANY EXPRESS OR IMPLIED WARRANTIES, INCLUDING, BUT NOT
LIMITED TO, THE IMPLIED WARRANTIES OF MERCHANTABILITY AND FITNESS FOR
A PARTICULAR PURPOSE ARE DISCLAIMED. IN NO EVENT SHALL THE COPYRIGHT
OWNER OR CONTRIBUTORS BE LIABLE FOR ANY DIRECT, INDIRECT, INCIDENTAL,
SPECIAL, EXEMPLARY, OR CONSEQUENTIAL DAMAGES (INCLUDING, BUT NOT
LIMITED TO, PROCUREMENT OF SUBSTITUTE GOODS OR SERVICES; LOSS OF USE,
DATA, OR PROFITS; OR BUSINESS INTERRUPTION) HOWEVER CAUSED AND ON ANY
THEORY OF LIABILITY, WHETHER IN CONTRACT, STRICT LIABILITY, OR TORT
(INCLUDING NEGLIGENCE OR OTHERWISE) ARISING IN ANY WAY OUT OF THE USE
OF THIS SOFTWARE, EVEN IF ADVISED OF THE POSSIBILITY OF SUCH DAMAGE.
`

type libraryData struct {
	Name        string
	ShortName   string
	LicenseURL  string
	LicenseName string
	Version     string
	License     string
}

func reportMain(_ *cobra.Command, args []string) error {
	classifier, err := licenses.NewClassifier(confidenceThreshold)
	if err != nil {
		return err
	}

	libs, err := licenses.Libraries(context.Background(), classifier, includeTests, ignore, args...)
	if err != nil {
		return err
	}

	var reportData []libraryData
	for _, lib := range libs {
		version := lib.Version()
		if len(version) == 0 {
			version = UNKNOWN
		}
		libData := libraryData{
			Name:        lib.Name(),
			ShortName:   lib.Name(),
			Version:     version,
			LicenseURL:  UNKNOWN,
			LicenseName: UNKNOWN,
			License:     UNKNOWN,
		}
		if lib.LicensePath != "" {
			name, _, err := classifier.Identify(lib.LicensePath)
			if err == nil {
				libData.LicenseName = name
			} else {
				klog.Errorf("Error identifying license in %q: %v", lib.LicensePath, err)
			}
			url, err := lib.FileURL(context.Background(), lib.LicensePath)
			if err == nil {
				libData.LicenseURL = url
				if strings.Contains(url, "github") {
					libData.ShortName = strings.Replace(lib.Name(), "github.com/", "", 1)
					url = strings.Replace(url, "github.com", "raw.githubusercontent.com", 1)
					url = strings.Replace(url, "blob/", "", 1)
				}
				if !strings.Contains(url, "opensource.google") {
					resp, err := http.Get(url)
					if err != nil {
						klog.Errorf("Error downloading license file from: %s, err: %v", url, err)
						continue
					}
					b, err := io.ReadAll(resp.Body)
					if err != nil {
						klog.Errorf("Error reading response body: %s, err: %v", url, err)
						continue
					}
					libData.License = string(b)
				} else {
					libData.License = fmt.Sprintf("<PLACEHOLDER_%s>", libData.LicenseName)
				}
			} else {
				klog.Warningf("Error discovering license URL: %s", err)
			}
		}
		reportData = append(reportData, libData)
	}

	if templateFile == "" {
		return reportCSV(reportData)
	} else {
		return reportTemplate(reportData)
	}
}

func reportCSV(libs []libraryData) error {
	writer := csv.NewWriter(os.Stdout)
	for _, lib := range libs {
		if err := writer.Write([]string{lib.Name, lib.LicenseURL, lib.LicenseName}); err != nil {
			return err
		}
	}
	writer.Flush()
	return writer.Error()
}

func reportTemplate(libs []libraryData) error {
	templateBytes, err := os.ReadFile(templateFile)
	if err != nil {
		return err
	}
	tmpl, err := template.New("").Parse(string(templateBytes))
	if err != nil {
		return err
	}
	return tmpl.Execute(os.Stdout, libs)
}
