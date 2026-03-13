package main

import (
	"os"
	"testing"

	"github.com/cucumber/godog"
)

func TestFeatures(t *testing.T) {
	pluginID := "plugin-wiz"

	baseURL := os.Getenv("API_BASE_URL")
	if baseURL == "" {
		t.Skip("API_BASE_URL not set; skipping external API feature suite")
	}

	suiteCtx := newAPIFeatureContext(t, baseURL, pluginID)

	suite := godog.TestSuite{
		Name: "plugin-wiz-features",
		ScenarioInitializer: func(ctx *godog.ScenarioContext) {
			suiteCtx.InitializeAllScenarios(ctx)
		},
		Options: &godog.Options{
			Format:   "pretty",
			Paths:    []string{"features"},
			TestingT: t,
		},
	}

	if suite.Run() != 0 {
		t.Fatal("Godog suite failed")
	}
}
