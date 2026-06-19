package core

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

func rawString(m map[string]interface{}, key string) (string, bool) {
	v, ok := m[key]
	if !ok {
		return "", false
	}
	if v == nil {
		return "", true
	}
	if s, ok := v.(string); ok {
		return s, true
	}
	return "", false
}

func rawFloat(m map[string]interface{}, key string) (float64, bool) {
	v, ok := m[key]
	if !ok {
		return 0, false
	}
	if v == nil {
		return 0, true
	}
	if f, ok := v.(float64); ok {
		return f, true
	}
	return 0, false
}

func rawInt(m map[string]interface{}, key string) (int, bool) {
	v, ok := m[key]
	if !ok {
		return 0, false
	}
	if v == nil {
		return 0, true
	}
	if f, ok := v.(float64); ok {
		return int(f), true
	}
	if s, ok := v.(string); ok {
		n, err := strconv.Atoi(s)
		if err == nil {
			return n, true
		}
	}
	return 0, false
}

func rawBool(m map[string]interface{}, key string) (bool, bool) {
	v, ok := m[key]
	if !ok {
		return false, false
	}
	if v == nil {
		return false, true
	}
	if b, ok := v.(bool); ok {
		return b, true
	}
	if f, ok := v.(float64); ok {
		return f != 0, true
	}
	if s, ok := v.(string); ok {
		return strings.EqualFold(s, "true") || s == "1", true
	}
	return false, false
}

func mergeJSONArray(fields map[string]interface{}, key, existing string) string {
	v, ok := fields[key]
	if !ok {
		return existing
	}
	if v == nil {
		return ""
	}
	if arr, ok := v.([]interface{}); ok {
		b, _ := json.Marshal(arr)
		return string(b)
	}
	if s, ok := v.(string); ok {
		b, _ := json.Marshal([]string{s})
		return string(b)
	}
	return existing
}

func mergeSocialLinks(fields map[string]interface{}, existing string) string {
	socialFieldNames := map[string]string{
		"instagram": "Instagram",
		"facebook":  "Facebook",
		"twitter":   "Twitter",
		"linkedin":  "LinkedIn",
		"tiktok":    "TikTok",
		"youtube":   "YouTube",
	}

	hasSocialKey := false
	for field := range socialFieldNames {
		if _, ok := fields[field]; ok {
			hasSocialKey = true
			break
		}
	}
	if _, ok := fields["social_links"]; ok {
		hasSocialKey = true
	}
	if !hasSocialKey {
		return existing
	}

	var links []map[string]string
	var existingLinks []SocialLink
	_ = json.Unmarshal([]byte(existing), &existingLinks)
	knownPlatforms := map[string]bool{}
	for _, l := range existingLinks {
		key := strings.ToLower(l.Platform)
		if !knownPlatforms[key] {
			links = append(links, map[string]string{"platform": l.Platform, "url": l.URL})
			knownPlatforms[key] = true
		}
	}

	for field, platform := range socialFieldNames {
		if v, ok := rawString(fields, field); ok && v != "" {
			key := strings.ToLower(platform)
			if !knownPlatforms[key] {
				links = append(links, map[string]string{"platform": platform, "url": v})
				knownPlatforms[key] = true
			} else {
				for i, l := range links {
					if strings.ToLower(l["platform"]) == key {
						links[i]["url"] = v
						break
					}
				}
			}
		}
	}

	if v, ok := fields["social_links"]; ok {
		if arr, ok := v.([]interface{}); ok {
			for _, item := range arr {
				if m, ok := item.(map[string]interface{}); ok {
					l := map[string]string{}
					if p, ok := m["platform"].(string); ok {
						l["platform"] = p
					}
					if u, ok := m["url"].(string); ok {
						l["url"] = u
					}
					if l["platform"] != "" || l["url"] != "" {
						key := strings.ToLower(l["platform"])
						if !knownPlatforms[key] {
							links = append(links, l)
							knownPlatforms[key] = true
						} else {
							for i, existingL := range links {
								if strings.ToLower(existingL["platform"]) == key {
									if l["url"] != "" {
										links[i]["url"] = l["url"]
									}
									break
								}
							}
						}
					}
				}
			}
		}
	}

	if len(links) > 0 {
		b, _ := json.Marshal(links)
		return string(b)
	}
	if v, ok := fields["social_links"]; ok && v == nil {
		return ""
	}
	return existing
}

func fmtSummary(index int, name, profession, city, rate string, years int) string {
	return fmt.Sprintf("%d. %s - %s in %s, %s, %d years experience", index, name, profession, city, rate, years)
}
