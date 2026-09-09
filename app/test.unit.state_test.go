package app

import (
	"reflect"
	"testing"

	domain "github.com/slidebolt/sb-domain"
)

func TestLightStateFromPilot_ColorTempMode(t *testing.T) {
	got := lightStateFromPilot(map[string]any{
		"state":   true,
		"dimming": float64(35),
		"temp":    float64(2200),
	})

	if !got.Power {
		t.Fatal("Power = false, want true")
	}
	if got.Brightness != 35 {
		t.Fatalf("Brightness = %d, want 35", got.Brightness)
	}
	if got.Temperature != 2200 {
		t.Fatalf("Temperature = %d, want 2200", got.Temperature)
	}
	if got.ColorMode != "color_temp" {
		t.Fatalf("ColorMode = %q, want color_temp", got.ColorMode)
	}
}

func TestLightStateFromPilot_RGBModeOverridesTemp(t *testing.T) {
	got := lightStateFromPilot(map[string]any{
		"state":   true,
		"dimming": float64(80),
		"temp":    float64(2700),
		"r":       float64(255),
		"g":       float64(128),
		"b":       float64(64),
	})

	if got.ColorMode != "rgb" {
		t.Fatalf("ColorMode = %q, want rgb", got.ColorMode)
	}
	if !reflect.DeepEqual(got.RGB, []int{255, 128, 64}) {
		t.Fatalf("RGB = %v, want [255 128 64]", got.RGB)
	}
	if got.Temperature != 2700 {
		t.Fatalf("Temperature = %d, want 2700", got.Temperature)
	}
}

func TestUpdateLightState_RGBSetsColorMode(t *testing.T) {
	entity := domain.Entity{
		State: domain.Light{
			Power:       false,
			Temperature: 2200,
			ColorMode:   "color_temp",
		},
	}

	state, _ := entity.State.(domain.Light)
	state.Power = true
	state.RGB = []int{1, 2, 3}
	state.ColorMode = "rgb"
	entity.State = state

	got, ok := entity.State.(domain.Light)
	if !ok {
		t.Fatalf("state type = %T, want domain.Light", entity.State)
	}
	if got.ColorMode != "rgb" {
		t.Fatalf("ColorMode = %q, want rgb", got.ColorMode)
	}
	if !reflect.DeepEqual(got.RGB, []int{1, 2, 3}) {
		t.Fatalf("RGB = %v, want [1 2 3]", got.RGB)
	}
}

func TestUpdateLightState_ColorTempClearsRGBAndSetsMode(t *testing.T) {
	entity := domain.Entity{
		State: domain.Light{
			Power:     true,
			RGB:       []int{255, 0, 0},
			ColorMode: "rgb",
		},
	}

	state, _ := entity.State.(domain.Light)
	state.Temperature = 2700
	state.ColorMode = "color_temp"
	state.RGB = nil
	entity.State = state

	got, ok := entity.State.(domain.Light)
	if !ok {
		t.Fatalf("state type = %T, want domain.Light", entity.State)
	}
	if got.ColorMode != "color_temp" {
		t.Fatalf("ColorMode = %q, want color_temp", got.ColorMode)
	}
	if got.RGB != nil {
		t.Fatalf("RGB = %v, want nil", got.RGB)
	}
	if got.Temperature != 2700 {
		t.Fatalf("Temperature = %d, want 2700", got.Temperature)
	}
}

func TestHSToRGB(t *testing.T) {
	tests := []struct {
		name       string
		hue        float64
		saturation float64
		want       []int
	}{
		{"red", 0, 100, []int{255, 0, 0}},
		{"green", 120, 100, []int{0, 255, 0}},
		{"blue", 240, 100, []int{0, 0, 255}},
		{"white", 240, 0, []int{255, 255, 255}},
		{"cyan half saturation", 180, 50, []int{128, 255, 255}},
		{"hue wraps", 360, 100, []int{255, 0, 0}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r, g, b := hsToRGB(tt.hue, tt.saturation)
			got := []int{r, g, b}
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("hsToRGB(%v, %v) = %v, want %v", tt.hue, tt.saturation, got, tt.want)
			}
		})
	}
}
