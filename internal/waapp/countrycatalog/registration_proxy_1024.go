// Package countrycatalog contains country metadata used only by runtime adapters.
package countrycatalog

import (
	"sort"
	"strings"
	"unicode"
)

// Country identifies a country-level registration region. It deliberately
// omits provider node addresses and subnational locations.
type Country struct {
	ISO2        string `json:"country_iso2"`
	EnglishName string `json:"english_name"`
}

// registrationProxy1024Countries is generated from the 1024proxy country
// catalogue supplied for this service. Rand is an access-node mode, not a
// country where a phone can be registered, so it is intentionally absent.
var registrationProxy1024Countries = []Country{
	{ISO2: "AD", EnglishName: "Andorra"},
	{ISO2: "AE", EnglishName: "United Arab Emirates"},
	{ISO2: "AF", EnglishName: "Afghanistan"},
	{ISO2: "AG", EnglishName: "Antigua & Barbuda"},
	{ISO2: "AI", EnglishName: "Anguilla"},
	{ISO2: "AL", EnglishName: "Albania"},
	{ISO2: "AM", EnglishName: "Armenia"},
	{ISO2: "AO", EnglishName: "Angola"},
	{ISO2: "AR", EnglishName: "Argentina"},
	{ISO2: "AT", EnglishName: "Austria"},
	{ISO2: "AU", EnglishName: "Australia"},
	{ISO2: "AZ", EnglishName: "Azerbaijan"},
	{ISO2: "BA", EnglishName: "Bosnia & Herzegovina"},
	{ISO2: "BB", EnglishName: "Barbados"},
	{ISO2: "BD", EnglishName: "Bangladesh"},
	{ISO2: "BE", EnglishName: "Belgium"},
	{ISO2: "BF", EnglishName: "Burkina Faso"},
	{ISO2: "BG", EnglishName: "Bulgaria"},
	{ISO2: "BH", EnglishName: "Bahrain"},
	{ISO2: "BI", EnglishName: "Burundi"},
	{ISO2: "BJ", EnglishName: "Benin"},
	{ISO2: "BM", EnglishName: "Bermuda"},
	{ISO2: "BN", EnglishName: "Brunei"},
	{ISO2: "BO", EnglishName: "Bolivia"},
	{ISO2: "BR", EnglishName: "Brazil"},
	{ISO2: "BS", EnglishName: "Bahamas"},
	{ISO2: "BT", EnglishName: "Bhutan"},
	{ISO2: "BW", EnglishName: "Botswana"},
	{ISO2: "BY", EnglishName: "Belarus"},
	{ISO2: "BZ", EnglishName: "Belize"},
	{ISO2: "CA", EnglishName: "Canada"},
	{ISO2: "CC", EnglishName: "Cocos (Keeling) Islands"},
	{ISO2: "CD", EnglishName: "Congo - Kinshasa"},
	{ISO2: "CF", EnglishName: "Central African Republic"},
	{ISO2: "CG", EnglishName: "Congo - Brazzaville"},
	{ISO2: "CH", EnglishName: "Switzerland"},
	{ISO2: "CI", EnglishName: "Cote d'Ivoire"},
	{ISO2: "CL", EnglishName: "Chile"},
	{ISO2: "CM", EnglishName: "Cameroon"},
	{ISO2: "CO", EnglishName: "Colombia"},
	{ISO2: "CR", EnglishName: "Costa Rica"},
	{ISO2: "CU", EnglishName: "Cuba"},
	{ISO2: "CV", EnglishName: "Cape Verde"},
	{ISO2: "CY", EnglishName: "Cyprus"},
	{ISO2: "CZ", EnglishName: "Czechia"},
	{ISO2: "DE", EnglishName: "Germany"},
	{ISO2: "DJ", EnglishName: "Djibouti"},
	{ISO2: "DK", EnglishName: "Denmark"},
	{ISO2: "DM", EnglishName: "Dominica"},
	{ISO2: "DO", EnglishName: "Dominican Republic"},
	{ISO2: "DZ", EnglishName: "Algeria"},
	{ISO2: "EC", EnglishName: "Ecuador"},
	{ISO2: "EE", EnglishName: "Estonia"},
	{ISO2: "EG", EnglishName: "Egypt"},
	{ISO2: "ES", EnglishName: "Spain"},
	{ISO2: "ET", EnglishName: "Ethiopia"},
	{ISO2: "FI", EnglishName: "Finland"},
	{ISO2: "FJ", EnglishName: "Fiji"},
	{ISO2: "FR", EnglishName: "France"},
	{ISO2: "GA", EnglishName: "Gabon"},
	{ISO2: "GB", EnglishName: "United Kingdom"},
	{ISO2: "GD", EnglishName: "Grenada"},
	{ISO2: "GE", EnglishName: "Georgia"},
	{ISO2: "GF", EnglishName: "French Guiana"},
	{ISO2: "GH", EnglishName: "Ghana"},
	{ISO2: "GI", EnglishName: "Gibraltar"},
	{ISO2: "GL", EnglishName: "Greenland"},
	{ISO2: "GM", EnglishName: "Gambia"},
	{ISO2: "GN", EnglishName: "Guinea"},
	{ISO2: "GP", EnglishName: "Guadeloupe"},
	{ISO2: "GQ", EnglishName: "Equatorial Guinea"},
	{ISO2: "GR", EnglishName: "Greece"},
	{ISO2: "GS", EnglishName: "South Georgia & South Sandwich Islands"},
	{ISO2: "GT", EnglishName: "Guatemala"},
	{ISO2: "GU", EnglishName: "Guam"},
	{ISO2: "GW", EnglishName: "Guinea-Bissau"},
	{ISO2: "GY", EnglishName: "Guyana"},
	{ISO2: "HK", EnglishName: "Hong Kong SAR China"},
	{ISO2: "HN", EnglishName: "Honduras"},
	{ISO2: "HR", EnglishName: "Croatia"},
	{ISO2: "HT", EnglishName: "Haiti"},
	{ISO2: "HU", EnglishName: "Hungary"},
	{ISO2: "ID", EnglishName: "Indonesia"},
	{ISO2: "IE", EnglishName: "Ireland"},
	{ISO2: "IL", EnglishName: "Israel"},
	{ISO2: "IN", EnglishName: "India"},
	{ISO2: "IQ", EnglishName: "Iraq"},
	{ISO2: "IR", EnglishName: "Iran"},
	{ISO2: "IS", EnglishName: "Iceland"},
	{ISO2: "IT", EnglishName: "Italy"},
	{ISO2: "JM", EnglishName: "Jamaica"},
	{ISO2: "JO", EnglishName: "Jordan"},
	{ISO2: "JP", EnglishName: "Japan"},
	{ISO2: "KE", EnglishName: "Kenya"},
	{ISO2: "KH", EnglishName: "Cambodia"},
	{ISO2: "KI", EnglishName: "Kiribati"},
	{ISO2: "KM", EnglishName: "Comoros"},
	{ISO2: "KN", EnglishName: "St. Kitts & Nevis"},
	{ISO2: "KR", EnglishName: "South Korea"},
	{ISO2: "KW", EnglishName: "Kuwait"},
	{ISO2: "KY", EnglishName: "Cayman Islands"},
	{ISO2: "KZ", EnglishName: "Kazakhstan"},
	{ISO2: "LA", EnglishName: "Laos"},
	{ISO2: "LB", EnglishName: "Lebanon"},
	{ISO2: "LC", EnglishName: "St. Lucia"},
	{ISO2: "LI", EnglishName: "Liechtenstein"},
	{ISO2: "LK", EnglishName: "Sri Lanka"},
	{ISO2: "LR", EnglishName: "Liberia"},
	{ISO2: "LS", EnglishName: "Lesotho"},
	{ISO2: "LT", EnglishName: "Lithuania"},
	{ISO2: "LU", EnglishName: "Luxembourg"},
	{ISO2: "LV", EnglishName: "Latvia"},
	{ISO2: "LY", EnglishName: "Libya"},
	{ISO2: "MC", EnglishName: "Monaco"},
	{ISO2: "MD", EnglishName: "Moldova"},
	{ISO2: "ME", EnglishName: "Montenegro"},
	{ISO2: "MG", EnglishName: "Madagascar"},
	{ISO2: "MH", EnglishName: "Marshall Islands"},
	{ISO2: "ML", EnglishName: "Mali"},
	{ISO2: "MM", EnglishName: "Myanmar (Burma)"},
	{ISO2: "MN", EnglishName: "Mongolia"},
	{ISO2: "MO", EnglishName: "Macao SAR China"},
	{ISO2: "MQ", EnglishName: "Martinique"},
	{ISO2: "MS", EnglishName: "Montserrat"},
	{ISO2: "MT", EnglishName: "Malta"},
	{ISO2: "MU", EnglishName: "Mauritius"},
	{ISO2: "MV", EnglishName: "Maldives"},
	{ISO2: "MW", EnglishName: "Malawi"},
	{ISO2: "MX", EnglishName: "Mexico"},
	{ISO2: "MY", EnglishName: "Malaysia"},
	{ISO2: "MZ", EnglishName: "Mozambique"},
	{ISO2: "NA", EnglishName: "Namibia"},
	{ISO2: "NC", EnglishName: "New Caledonia"},
	{ISO2: "NE", EnglishName: "Niger"},
	{ISO2: "NF", EnglishName: "Norfolk Island"},
	{ISO2: "NG", EnglishName: "Nigeria"},
	{ISO2: "NI", EnglishName: "Nicaragua"},
	{ISO2: "NL", EnglishName: "Netherlands"},
	{ISO2: "NO", EnglishName: "Norway"},
	{ISO2: "NP", EnglishName: "Nepal"},
	{ISO2: "NR", EnglishName: "Nauru"},
	{ISO2: "NZ", EnglishName: "New Zealand"},
	{ISO2: "OM", EnglishName: "Oman"},
	{ISO2: "PA", EnglishName: "Panama"},
	{ISO2: "PE", EnglishName: "Peru"},
	{ISO2: "PF", EnglishName: "French Polynesia"},
	{ISO2: "PH", EnglishName: "Philippines"},
	{ISO2: "PK", EnglishName: "Pakistan"},
	{ISO2: "PL", EnglishName: "Poland"},
	{ISO2: "PN", EnglishName: "Pitcairn Islands"},
	{ISO2: "PR", EnglishName: "Puerto Rico"},
	{ISO2: "PS", EnglishName: "Palestinian Territories"},
	{ISO2: "PT", EnglishName: "Portugal"},
	{ISO2: "PW", EnglishName: "Palau"},
	{ISO2: "PY", EnglishName: "Paraguay"},
	{ISO2: "QA", EnglishName: "Qatar"},
	{ISO2: "RO", EnglishName: "Romania"},
	{ISO2: "RS", EnglishName: "Serbia"},
	{ISO2: "RU", EnglishName: "Russia"},
	{ISO2: "RW", EnglishName: "Rwanda"},
	{ISO2: "SA", EnglishName: "Saudi Arabia"},
	{ISO2: "SB", EnglishName: "Solomon Islands"},
	{ISO2: "SC", EnglishName: "Seychelles"},
	{ISO2: "SD", EnglishName: "Sudan"},
	{ISO2: "SE", EnglishName: "Sweden"},
	{ISO2: "SG", EnglishName: "Singapore"},
	{ISO2: "SI", EnglishName: "Slovenia"},
	{ISO2: "SK", EnglishName: "Slovakia"},
	{ISO2: "SL", EnglishName: "Sierra Leone"},
	{ISO2: "SM", EnglishName: "San Marino"},
	{ISO2: "SN", EnglishName: "Senegal"},
	{ISO2: "SO", EnglishName: "Somalia"},
	{ISO2: "SR", EnglishName: "Suriname"},
	{ISO2: "SS", EnglishName: "South Sudan"},
	{ISO2: "ST", EnglishName: "Sao Tome & Principe"},
	{ISO2: "SV", EnglishName: "El Salvador"},
	{ISO2: "SY", EnglishName: "Syria"},
	{ISO2: "SZ", EnglishName: "Eswatini"},
	{ISO2: "TC", EnglishName: "Turks & Caicos Islands"},
	{ISO2: "TD", EnglishName: "Chad"},
	{ISO2: "TG", EnglishName: "Togo"},
	{ISO2: "TH", EnglishName: "Thailand"},
	{ISO2: "TJ", EnglishName: "Tajikistan"},
	{ISO2: "TL", EnglishName: "Timor-Leste"},
	{ISO2: "TM", EnglishName: "Turkmenistan"},
	{ISO2: "TN", EnglishName: "Tunisia"},
	{ISO2: "TO", EnglishName: "Tonga"},
	{ISO2: "TR", EnglishName: "Turkiye"},
	{ISO2: "TT", EnglishName: "Trinidad & Tobago"},
	{ISO2: "TW", EnglishName: "Taiwan"},
	{ISO2: "TZ", EnglishName: "Tanzania"},
	{ISO2: "UA", EnglishName: "Ukraine"},
	{ISO2: "UG", EnglishName: "Uganda"},
	{ISO2: "US", EnglishName: "United States"},
	{ISO2: "UY", EnglishName: "Uruguay"},
	{ISO2: "UZ", EnglishName: "Uzbekistan"},
	{ISO2: "VC", EnglishName: "St. Vincent & Grenadines"},
	{ISO2: "VE", EnglishName: "Venezuela"},
	{ISO2: "VG", EnglishName: "British Virgin Islands"},
	{ISO2: "VI", EnglishName: "U.S. Virgin Islands"},
	{ISO2: "VN", EnglishName: "Vietnam"},
	{ISO2: "VU", EnglishName: "Vanuatu"},
	{ISO2: "WF", EnglishName: "Wallis & Futuna"},
	{ISO2: "WS", EnglishName: "Samoa"},
	{ISO2: "YE", EnglishName: "Yemen"},
	{ISO2: "ZA", EnglishName: "South Africa"},
	{ISO2: "ZM", EnglishName: "Zambia"},
	{ISO2: "ZW", EnglishName: "Zimbabwe"},
}

var countryByISO2 map[string]Country
var countryByNormalizedName map[string]string

var countryNameAliases = map[string]string{
	"bosnia":                       "BA",
	"czech":                        "CZ",
	"ivorycoast":                   "CI",
	"macao":                        "MO",
	"republicofthecongo":           "CG",
	"salvador":                     "SV",
	"saintkittsandnevis":           "KN",
	"saintlucia":                   "LC",
	"saintvincentandthegrenadines": "VC",
	"saotomeandprincipe":           "ST",
	"swaziland":                    "SZ",
	"trinidadandtobago":            "TT",
	"turkey":                       "TR",
	"uae":                          "AE",
	"unitedstatesofamerica":        "US",
	"usa":                          "US",
}

func init() {
	countryByISO2 = make(map[string]Country, len(registrationProxy1024Countries))
	countryByNormalizedName = make(map[string]string, len(registrationProxy1024Countries)+len(countryNameAliases))
	for _, country := range registrationProxy1024Countries {
		countryByISO2[country.ISO2] = country
		countryByNormalizedName[normalizeName(country.EnglishName)] = country.ISO2
	}
	for name, iso2 := range countryNameAliases {
		countryByNormalizedName[normalizeName(name)] = iso2
	}
}

// RegistrationProxy1024Countries returns a stable copy of the supported
// country-level 1024proxy exit regions.
func RegistrationProxy1024Countries() []Country {
	result := make([]Country, len(registrationProxy1024Countries))
	copy(result, registrationProxy1024Countries)
	sort.Slice(result, func(left, right int) bool {
		return result[left].ISO2 < result[right].ISO2
	})
	return result
}

func SupportsRegistrationProxy1024Country(value string) bool {
	_, ok := countryByISO2[strings.ToUpper(strings.TrimSpace(value))]
	return ok
}

// ISO2FromCountryNames resolves an upstream country name to a supported ISO2
// code. The caller can pass names in preference order.
func ISO2FromCountryNames(values ...string) string {
	for _, value := range values {
		if iso2, ok := countryByNormalizedName[normalizeName(value)]; ok {
			return iso2
		}
	}
	return ""
}

func normalizeName(value string) string {
	var result strings.Builder
	for _, runeValue := range strings.ToLower(strings.TrimSpace(value)) {
		if unicode.IsLetter(runeValue) || unicode.IsDigit(runeValue) {
			result.WriteRune(runeValue)
		}
	}
	return result.String()
}
