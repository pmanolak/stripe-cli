package plugins

import (
	"testing"

	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
)

func TestUpdatesEnabled(t *testing.T) {
	tests := []struct {
		name       string
		pluginVal  string
		globalVal  string
		pluginName string
		want       bool
	}{
		{
			name: "no config set defaults to off",
			want: false,
		},
		{
			name:      "global on enables updates",
			globalVal: "on",
			want:      true,
		},
		{
			name:      "global off disables updates",
			globalVal: "off",
			want:      false,
		},
		{
			name:       "plugin-specific on overrides global off",
			pluginVal:  "on",
			globalVal:  "off",
			pluginName: "apps",
			want:       true,
		},
		{
			name:       "plugin-specific off overrides global on",
			pluginVal:  "off",
			globalVal:  "on",
			pluginName: "apps",
			want:       false,
		},
		{
			name:       "plugin-specific on with no global",
			pluginVal:  "on",
			pluginName: "apps",
			want:       true,
		},
		{
			name:       "plugin-specific off with no global",
			pluginVal:  "off",
			pluginName: "apps",
			want:       false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			viper.Reset()
			t.Cleanup(viper.Reset)

			pluginName := tt.pluginName
			if pluginName == "" {
				pluginName = "apps"
			}

			if tt.pluginVal != "" {
				viper.Set("plugin_configs."+pluginName+".updates", tt.pluginVal)
			}
			if tt.globalVal != "" {
				viper.Set("plugin_configs.__global.updates", tt.globalVal)
			}

			assert.Equal(t, tt.want, updatesEnabled(pluginName))
		})
	}
}
