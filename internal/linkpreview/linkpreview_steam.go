package linkpreview

import (
	"context"
	"encoding/json"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

// Steam store pages carry a title, description and image as Open Graph tags,
// but not what people expect on the card: the price and how well the game is
// received. The store's own JSON API has both, so this one provider is read
// from it. The code is isolated here; everything else about the card comes
// from the generic path, and any failure here falls back to it.

// steamCountry picks the currency the price is shown in. The cache is per
// link, not per member, so one currency serves everybody.
const steamCountry = "us"

const steamColor = "#1b2838"

var steamAppPath = regexp.MustCompile(`^/app/([0-9]{1,10})(?:/|$)`)

// steamAppID returns the app id of a store.steampowered.com/app/ID link.
func steamAppID(u *url.URL) string {
	if strings.ToLower(u.Hostname()) != "store.steampowered.com" {
		return ""
	}
	if m := steamAppPath.FindStringSubmatch(u.Path); m != nil {
		return m[1]
	}
	return ""
}

// steamPrice is the store's formatted price in steamCountry's currency.
type steamPrice struct {
	DiscountPercent int    `json:"discount_percent"`
	InitialFmt      string `json:"initial_formatted"`
	FinalFmt        string `json:"final_formatted"`
}

type steamAnswer map[string]struct {
	Success bool `json:"success"`
	Data    struct {
		Name             string      `json:"name"`
		ShortDescription string      `json:"short_description"`
		HeaderImage      string      `json:"header_image"`
		IsFree           bool        `json:"is_free"`
		Developers       []string    `json:"developers"`
		PriceOverview    *steamPrice `json:"price_overview"`
		Recommendations  *struct {
			Total int `json:"total"`
		} `json:"recommendations"`
		ReleaseDate *struct {
			ComingSoon bool   `json:"coming_soon"`
			Date       string `json:"date"`
		} `json:"release_date"`
	} `json:"data"`
}

// steamPreview builds the card from the store API. It returns nil (no error)
// when the app is unknown or the API is unavailable.
func (f *Fetcher) steamPreview(ctx context.Context, page *url.URL, appID string) (*Preview, error) {
	endpoint, err := url.Parse("https://store.steampowered.com/api/appdetails?appids=" + appID + "&cc=" + steamCountry + "&l=english")
	if err != nil {
		return nil, nil
	}
	body, _, _, err := f.get(ctx, endpoint, "application/json", 512<<10)
	if err != nil {
		if isBusy(err) {
			return nil, err
		}
		return nil, nil
	}
	return steamCard(body, page, appID), nil
}

// steamCard turns an appdetails answer into a card, or nil.
func steamCard(body []byte, page *url.URL, appID string) *Preview {
	var ans steamAnswer
	if json.Unmarshal(body, &ans) != nil {
		return nil
	}
	app, ok := ans[appID]
	if !ok || !app.Success || strings.TrimSpace(app.Data.Name) == "" {
		return nil
	}
	d := app.Data
	p := &Preview{
		URL:         page.String(),
		Title:       clip(d.Name, maxTitleRunes),
		Description: clip(d.ShortDescription, maxDescRunes),
		SiteName:    "Steam",
		Color:       steamColor,
		Image:       absoluteHTTP(page, d.HeaderImage),
	}
	if len(d.Developers) > 0 {
		p.Author = clip(d.Developers[0], 100)
	}
	if price := priceText(d.IsFree, d.PriceOverview); price != "" {
		p.Fields = append(p.Fields, Field{Key: "price", Name: "Price", Value: price})
	}
	if d.Recommendations != nil && d.Recommendations.Total > 0 {
		p.Fields = append(p.Fields, Field{Key: "recommendations", Name: "Recommendations", Value: groupDigits(d.Recommendations.Total)})
	}
	return p
}

func priceText(free bool, po *steamPrice) string {
	if free {
		return "Free"
	}
	if po == nil || po.FinalFmt == "" {
		return ""
	}
	if po.DiscountPercent > 0 && po.InitialFmt != "" {
		return clip("-"+strconv.Itoa(po.DiscountPercent)+"% "+po.FinalFmt+" (was "+po.InitialFmt+")", 60)
	}
	return clip(po.FinalFmt, 40)
}

// groupDigits formats 12345 as "12,345".
func groupDigits(n int) string {
	s := strconv.Itoa(n)
	for i := len(s) - 3; i > 0; i -= 3 {
		s = s[:i] + "," + s[i:]
	}
	return s
}
