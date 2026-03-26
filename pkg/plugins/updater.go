package plugins

import (
	"context"
	"path/filepath"
	"sync"

	"github.com/hashicorp/go-version"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/afero"
	"github.com/spf13/viper"

	"github.com/stripe/stripe-cli/pkg/config"
	"github.com/stripe/stripe-cli/pkg/stripe"
)

var backgroundUpdates sync.WaitGroup

// UpdateResult holds the outcome of a background plugin update.
// Version is empty when no update was needed or available.
type UpdateResult struct {
	Version string
	Err     error
}

// WaitForBackgroundUpdates blocks until all in-flight background plugin
// downloads have completed. Call this before the process exits.
func WaitForBackgroundUpdates() {
	backgroundUpdates.Wait()
}

// updatesEnabled reports whether automatic updates are enabled for the given
// plugin. It checks the plugin-specific config first, then the global config.
// The default when neither is set is false (updates off).
func updatesEnabled(pluginName string) bool {
	logger := log.WithFields(log.Fields{"prefix": "plugins.updater"})
	pluginVal := viper.GetString(PluginConfigKey(pluginName, PluginConfigUpdatesField))
	if pluginVal != "" {
		logger.Debugf("Automatic updates for plugin '%s' enabled: %t", pluginName, pluginVal == "on")
		return pluginVal == "on"
	}
	globalVal := viper.GetString(PluginConfigKey(PluginConfigGlobalScope, PluginConfigUpdatesField))
	logger.Debugf("Automatic updates for plugins globally enabled: %t", globalVal == "on")
	return globalVal == "on"
}

// CheckAndUpdateInBackground refreshes the plugin manifest and, if a newer
// version is available, downloads it in a background goroutine so the current
// invocation is not delayed.
//
// runDone is closed by the caller after the current plugin process has finished;
// the old plugin versions are not removed until that signal is received.
//
// The returned channel receives an UpdateResult when the check (and any
// download) completes. Version is empty if no update was needed.
func CheckAndUpdateInBackground(ctx context.Context, cfg config.IConfig, fs afero.Fs, p *Plugin, runDone <-chan struct{}) <-chan UpdateResult {
	// Buffered so the goroutine never blocks when sending the result.
	resultCh := make(chan UpdateResult, 1)

	if !updatesEnabled(p.Shortname) {
		resultCh <- UpdateResult{}
		return resultCh
	}

	currentVersion, err := installedPluginVersion(cfg, p)
	if err != nil || currentVersion == "" || currentVersion == "local.build.dev" {
		resultCh <- UpdateResult{}
		return resultCh
	}

	// Use a detached context so work is not canceled when the parent
	// command context is done.
	backgroundUpdates.Go(func() {
		logger := log.WithFields(log.Fields{"prefix": "plugins.updater"})

		if err := RefreshPluginManifest(context.Background(), cfg, fs, stripe.DefaultAPIBaseURL); err != nil {
			logger.Debugf("Could not refresh plugin manifest: %s", err)
			resultCh <- UpdateResult{}
			return
		}

		// Re-look up the plugin so we have the freshest release list.
		fresh, err := LookUpPlugin(context.Background(), cfg, fs, p.Shortname)
		if err != nil {
			resultCh <- UpdateResult{}
			return
		}

		latestVersion := fresh.LookUpLatestVersion()
		if latestVersion == "" {
			resultCh <- UpdateResult{}
			return
		}

		current, err := version.NewVersion(currentVersion)
		if err != nil {
			resultCh <- UpdateResult{}
			return
		}

		latest, err := version.NewVersion(latestVersion)
		if err != nil {
			resultCh <- UpdateResult{}
			return
		}

		if !latest.GreaterThan(current) {
			resultCh <- UpdateResult{}
			return
		}

		logger.Debugf("Updating plugin '%s' from %s to %s in background", p.Shortname, currentVersion, latestVersion)

		installErr := fresh.install(context.Background(), cfg, fs, latestVersion, stripe.DefaultAPIBaseURL, false)
		resultCh <- UpdateResult{Version: latestVersion, Err: installErr}
		if installErr != nil {
			logger.Debugf("Background update of plugin '%s' failed: %s", p.Shortname, installErr)
			return
		}

		// Wait until the current plugin invocation has finished before removing
		// the old version so the running binary is not deleted mid-execution.
		<-runDone
		fresh.cleanUpPluginPath(cfg, fs, latestVersion)
	})

	return resultCh
}

// installedPluginVersion returns the version string of the locally installed
// plugin binary, or an empty string if no installation is found.
func installedPluginVersion(cfg config.IConfig, p *Plugin) (string, error) {
	pattern := filepath.Join(getPluginsDir(cfg), p.Shortname, "*.*.*")
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return "", err
	}
	if len(matches) == 0 {
		return "", nil
	}
	return filepath.Base(matches[0]), nil
}
