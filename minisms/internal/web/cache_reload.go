// Architected and Developed by :- Faisal Hanif | imfanee@gmail.com.
package web

import "context"

func (h *Handlers) reloadRouteCache(ctx context.Context) {
	if h.RouteCache == nil {
		return
	}
	_ = h.RouteCache.Reload(ctx, h.Pool)
}
