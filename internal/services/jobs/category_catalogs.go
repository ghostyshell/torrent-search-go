package jobs

// CategoryDef describes one browsable adult-content category.
type CategoryDef struct {
	Slug      string
	Name      string
	StashTag  string
	TPDBQuery string
	Default   bool
}

// AllCategories is the curated category list shared by TPDB and StashDB.
var AllCategories = []CategoryDef{
	{Slug: "milf", Name: "MILF", StashTag: "MILF", TPDBQuery: "MILF", Default: true},
	{Slug: "anal", Name: "Anal", StashTag: "Anal", TPDBQuery: "Anal", Default: true},
	{Slug: "teen", Name: "Teen", StashTag: "Teen", TPDBQuery: "Teen", Default: true},
	{Slug: "lesbian", Name: "Lesbian", StashTag: "Lesbian", TPDBQuery: "Lesbian", Default: true},
	{Slug: "threesome", Name: "Threesome", StashTag: "Threesome", TPDBQuery: "Threesome", Default: true},
	{Slug: "big-tits", Name: "Big Tits", StashTag: "Big Tits", TPDBQuery: "Big Tits", Default: true},
	{Slug: "creampie", Name: "Creampie", StashTag: "Creampie", TPDBQuery: "Creampie", Default: true},
	{Slug: "interracial", Name: "Interracial", StashTag: "Interracial", TPDBQuery: "Interracial", Default: true},
	{Slug: "pov", Name: "POV", StashTag: "POV", TPDBQuery: "POV", Default: true},
	{Slug: "blowjob", Name: "Blowjob", StashTag: "Blowjob", TPDBQuery: "Blowjob", Default: true},
	{Slug: "asian", Name: "Asian", StashTag: "Asian", TPDBQuery: "Asian", Default: true},
	{Slug: "latina", Name: "Latina", StashTag: "Latina", TPDBQuery: "Latina", Default: true},
	{Slug: "ebony", Name: "Ebony", StashTag: "Ebony", TPDBQuery: "Ebony", Default: false}, // ponytail: opt-in, not a shared-default split - pr_tag lists it regardless (1552 results); tpdb/stashdb Ebony is sparse (52/3) so it is off by default. See the StashDB configure-page note.
	{Slug: "mature", Name: "Mature", StashTag: "Mature", TPDBQuery: "Mature", Default: true},
	{Slug: "gangbang", Name: "Gangbang", StashTag: "Gangbang", TPDBQuery: "Gangbang", Default: true},
	{Slug: "double-penetration", Name: "Double Penetration", StashTag: "Double Penetration", TPDBQuery: "Double Penetration", Default: true},
	{Slug: "hardcore", Name: "Hardcore", StashTag: "Hardcore", TPDBQuery: "Hardcore", Default: true},
	{Slug: "big-ass", Name: "Big Ass", StashTag: "Big Ass", TPDBQuery: "Big Ass", Default: true},

	{Slug: "public", Name: "Public", StashTag: "Public", TPDBQuery: "Public", Default: false},
	{Slug: "cosplay", Name: "Cosplay", StashTag: "Cosplay", TPDBQuery: "Cosplay", Default: false},
	{Slug: "bdsm", Name: "BDSM", StashTag: "BDSM", TPDBQuery: "BDSM", Default: false},
	{Slug: "rough-sex", Name: "Rough Sex", StashTag: "Rough Sex", TPDBQuery: "Rough Sex", Default: false},
	{Slug: "squirting", Name: "Squirting", StashTag: "Squirting", TPDBQuery: "Squirting", Default: false},
	{Slug: "massage", Name: "Massage", StashTag: "Massage", TPDBQuery: "Massage", Default: false},
	{Slug: "facial", Name: "Facial", StashTag: "Facial", TPDBQuery: "Facial", Default: false},
	{Slug: "orgy", Name: "Orgy", StashTag: "Orgy", TPDBQuery: "Orgy", Default: false},
	{Slug: "bukkake", Name: "Bukkake", StashTag: "Bukkake", TPDBQuery: "Bukkake", Default: false},
	{Slug: "feet", Name: "Feet", StashTag: "Feet", TPDBQuery: "Feet", Default: false},
	{Slug: "redhead", Name: "Redhead", StashTag: "Redhead", TPDBQuery: "Redhead", Default: false},
	{Slug: "blonde", Name: "Blonde", StashTag: "Blonde", TPDBQuery: "Blonde", Default: false},
	{Slug: "petite", Name: "Petite", StashTag: "Petite", TPDBQuery: "Petite", Default: false},
	{Slug: "bbw", Name: "BBW", StashTag: "BBW", TPDBQuery: "BBW", Default: false},
	{Slug: "tattoo", Name: "Tattoo", StashTag: "Tattoo", TPDBQuery: "Tattoo", Default: false},
	{Slug: "stepfamily", Name: "Stepfamily", StashTag: "Stepfamily", TPDBQuery: "Stepfamily", Default: false},
	{Slug: "cuckold", Name: "Cuckold", StashTag: "Cuckold", TPDBQuery: "Cuckold", Default: false},
	{Slug: "voyeur", Name: "Voyeur", StashTag: "Voyeur", TPDBQuery: "Voyeur", Default: false},
	{Slug: "handjob", Name: "Handjob", StashTag: "Handjob", TPDBQuery: "Handjob", Default: false},
	{Slug: "deepthroat", Name: "Deepthroat", StashTag: "Deepthroat", TPDBQuery: "Deepthroat", Default: false},
	{Slug: "fisting", Name: "Fisting", StashTag: "Fisting", TPDBQuery: "Fisting", Default: false},
	{Slug: "pissing", Name: "Pissing", StashTag: "Pissing", TPDBQuery: "Pissing", Default: false},
}

// OrderedCategories returns default-enabled categories first.
func OrderedCategories() []CategoryDef {
	def := make([]CategoryDef, 0, len(AllCategories))
	rest := make([]CategoryDef, 0, len(AllCategories))
	for _, c := range AllCategories {
		if c.Default {
			def = append(def, c)
		} else {
			rest = append(rest, c)
		}
	}
	return append(def, rest...)
}
