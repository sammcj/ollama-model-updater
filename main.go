package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"

	"github.com/manifoldco/promptui"
	"github.com/ollama/ollama/api"
	"github.com/spf13/pflag"
)

type model struct {
	Name   string
	Digest string
}

func main() {
	pflag.BoolP("interactive", "i", true, "interactive mode")
	pflag.BoolP("force", "f", false, "force update all models")
	pflag.Parse()

	client, err := api.ClientFromEnvironment()
	if err != nil {
		log.Fatal(err)
	}

	ctx := context.Background()

	localModels, err := client.List(ctx)
	if err != nil {
		log.Fatal(err)
	}

	localModelsRaw := make([]model, len(localModels.Models))
	for i, m := range localModels.Models {
		localModelsRaw[i] = model{Name: m.Name, Digest: m.Digest}
	}

	updatableModels := make([]model, 0)

	for _, m := range localModelsRaw {
		repoAndTag := strings.Split(m.Name, ":")
		if !strings.Contains(repoAndTag[0], "/") {
			repoAndTag[0] = "library/" + repoAndTag[0]
		}

		req, err := http.NewRequest("GET", fmt.Sprintf("https://ollama.ai/v2/%s/manifests/%s", repoAndTag[0], repoAndTag[1]), nil)
		if err != nil {
			log.Fatal(err)
		}
		req.Header.Set("Accept", "application/vnd.docker.distribution.manifest.v2+json")

		httpClient := &http.Client{}
		resp, err := httpClient.Do(req)
		if err != nil {
			log.Fatal(err)
		}
		defer resp.Body.Close()

		if resp.StatusCode == 200 {
			var remoteModelInfo struct {
				Config struct {
					Digest string `json:"digest"`
				} `json:"config"`
			}
			err := json.NewDecoder(resp.Body).Decode(&remoteModelInfo)
			if err != nil {
				log.Fatal(err)
			}

			hash := jsonHash(remoteModelInfo)
			if hash != m.Digest {
				updatableModels = append(updatableModels, m)
			}
		}
	}

	if len(updatableModels) == 0 {
		fmt.Println("All models are up to date!")
		return
	}

	if pflag.Lookup("force").Changed {
		for _, m := range updatableModels {
			fmt.Printf("Updating %s\n", m.Name)
			pullModel(client, ctx, m)
		}
	} else {
		prompt := &promptui.Select{
			Label: "Select models to update",
			Items: updatableModels,
			Templates: &promptui.SelectTemplates{
				Label:    "{{ . }}",
				Active:   "{{ .Name | cyan }}",
				Inactive: "{{ .Name | cyan }}",
				Details: `
{{ .Name | cyan }}
`,
			},
		}

		for {
			_, result, err := prompt.Run()

			if err != nil {
				log.Fatal(err)
			}

			fmt.Printf("Updating %s\n", result)
			pullModel(client, ctx, model{Name: result}) // Convert result to model type
		}
	}
}

func pullModel(client *api.Client, ctx context.Context, m model) {
	pullReq := &api.PullRequest{
		Model: m.Name,
	}

	progressFunc := func(resp api.ProgressResponse) error {
		fmt.Printf("\rProgress: %v/%v (%.2f%%)", resp.Completed, resp.Total, float64(resp.Completed)/float64(resp.Total)*100)
		return nil
	}

	err := client.Pull(ctx, pullReq, progressFunc)
	if err != nil {
		log.Fatal(err)
	}
}

func jsonHash(jsonStruct interface{}) string {
	bytes, err := json.Marshal(jsonStruct)
	if err != nil {
		return ""
	}

	hash := sha256.Sum256(bytes)
	return hex.EncodeToString(hash[:])
}
