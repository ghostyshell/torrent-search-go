package stremio

// DebridProvider describes a supported debrid service for manifest id generation.
type DebridProvider struct {
	Field string
	Token string
	Label string
}

// DebridProviders is the priority order when multiple keys are present in a malformed config.
var DebridProviders = []DebridProvider{
	{Field: "rdKey", Token: "rd", Label: "Real-Debrid"},
	{Field: "tbKey", Token: "tb", Label: "TorBox"},
	{Field: "pmKey", Token: "pm", Label: "Premiumize"},
	{Field: "edKey", Token: "ed", Label: "EasyDebrid"},
	{Field: "dlKey", Token: "dl", Label: "Debrid-Link"},
	{Field: "ocKey", Token: "oc", Label: "Offcloud"},
	{Field: "puKey", Token: "pu", Label: "Put.io"},
	{Field: "dpKey", Token: "dp", Label: "Deepbrid"},
	{Field: "lsKey", Token: "ls", Label: "LinkSnappy"},
	{Field: "mgKey", Token: "mg", Label: "Mega-Debrid"},
	{Field: "drKey", Token: "dr", Label: "Debrider"},
	{Field: "srKey", Token: "sr", Label: "Seedr"},
}

// DebridKeyFields lists config keys in debrid priority order.
var DebridKeyFields = []string{
	"rdKey", "tbKey", "pmKey", "edKey", "dlKey", "ocKey", "puKey",
	"dpKey", "lsKey", "mgKey", "drKey", "srKey",
}

// DetectProvider returns the single active provider for a parsed config, or nil for P2P-only.
func DetectProvider(cfg Config) *DebridProvider {
	for i := range DebridProviders {
		p := &DebridProviders[i]
		if cfg.debridKey(p.Field) != "" {
			return p
		}
	}
	return nil
}

func (c Config) debridKey(field string) string {
	switch field {
	case "rdKey":
		return c.RdKey
	case "tbKey":
		return c.TbKey
	case "pmKey":
		return c.PmKey
	case "edKey":
		return c.EdKey
	case "dlKey":
		return c.DlKey
	case "ocKey":
		return c.OcKey
	case "puKey":
		return c.PuKey
	case "dpKey":
		return c.DpKey
	case "lsKey":
		return c.LsKey
	case "mgKey":
		return c.MgKey
	case "drKey":
		return c.DrKey
	case "srKey":
		return c.SrKey
	default:
		return ""
	}
}

func (c *Config) setDebridKey(field, value string) {
	switch field {
	case "rdKey":
		c.RdKey = value
	case "tbKey":
		c.TbKey = value
	case "pmKey":
		c.PmKey = value
	case "edKey":
		c.EdKey = value
	case "dlKey":
		c.DlKey = value
	case "ocKey":
		c.OcKey = value
	case "puKey":
		c.PuKey = value
	case "dpKey":
		c.DpKey = value
	case "lsKey":
		c.LsKey = value
	case "mgKey":
		c.MgKey = value
	case "drKey":
		c.DrKey = value
	case "srKey":
		c.SrKey = value
	}
}
