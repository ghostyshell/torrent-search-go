package jobs

import "context"

// HentaiSync is the background job that discovers new HentaiMama series. One
// tick runs HentaiIngest (no separate enrich step - the source rating is
// stored directly, no external MAL lookup). Same name/shape slot as
// PornripsSync so the scheduler treats it uniformly.
func (r *Runner) HentaiSync(ctx context.Context) (map[string]interface{}, error) {
	return r.HentaiIngest(ctx)
}