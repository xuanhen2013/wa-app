package countrycatalog

import "testing"

func TestRegistrationProxy1024CountriesExcludeRandAndResolveHeroAliases(t *testing.T) {
	countries := RegistrationProxy1024Countries()
	if len(countries) != 208 {
		t.Fatalf("country count=%d, want 208", len(countries))
	}
	if SupportsRegistrationProxy1024Country("Rand") {
		t.Fatal("Rand is an access-node mode, not a registration country")
	}
	for _, input := range []struct {
		name string
		want string
	}{
		{name: "Philippines", want: "PH"},
		{name: "USA", want: "US"},
		{name: "Czech", want: "CZ"},
		{name: "Trinidad and Tobago", want: "TT"},
	} {
		if got := ISO2FromCountryNames(input.name); got != input.want {
			t.Fatalf("ISO2FromCountryNames(%q)=%q, want %q", input.name, got, input.want)
		}
	}
}
