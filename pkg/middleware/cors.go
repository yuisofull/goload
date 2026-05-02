package middleware

import (
	"net/http"
	"strconv"
	"strings"
)

// CORSOptions controls behavior for CORS response headers and preflight handling.
type CORSOptions struct {
	AllowedOrigins   []string
	AllowedMethods   []string
	AllowedHeaders   []string
	ExposedHeaders   []string
	AllowCredentials bool
	PreflightMaxAge  int
}

// CORSHTTPMiddleware applies CORS headers and handles valid preflight requests.
func CORSHTTPMiddleware(opts CORSOptions) func(http.Handler) http.Handler {
	allowedOrigins := withDefault(opts.AllowedOrigins, []string{"*"})
	allowedMethods := withDefault(
		opts.AllowedMethods,
		[]string{
			http.MethodGet,
			http.MethodPost,
			http.MethodPut,
			http.MethodPatch,
			http.MethodDelete,
			http.MethodOptions,
		},
	)
	allowedHeaders := withDefault(opts.AllowedHeaders, []string{"Authorization", "Content-Type", "Accept", "Origin"})
	allowAllOrigins := containsIgnoreCase(allowedOrigins, "*")
	allowMethodsHeader := strings.Join(allowedMethods, ", ")
	allowHeadersHeader := strings.Join(allowedHeaders, ", ")
	exposeHeadersHeader := strings.Join(opts.ExposedHeaders, ", ")

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")
			isPreflight := r.Method == http.MethodOptions && r.Header.Get("Access-Control-Request-Method") != ""

			if origin != "" {
				w.Header().Add("Vary", "Origin")
			}
			if isPreflight {
				w.Header().Add("Vary", "Access-Control-Request-Method")
				w.Header().Add("Vary", "Access-Control-Request-Headers")
			}

			allowedOrigin := resolveAllowedOrigin(origin, allowedOrigins, allowAllOrigins, opts.AllowCredentials)
			if allowedOrigin != "" {
				w.Header().Set("Access-Control-Allow-Origin", allowedOrigin)
				if opts.AllowCredentials {
					w.Header().Set("Access-Control-Allow-Credentials", "true")
				}
				if exposeHeadersHeader != "" {
					w.Header().Set("Access-Control-Expose-Headers", exposeHeadersHeader)
				}
			}

			if isPreflight {
				if allowedOrigin == "" {
					w.WriteHeader(http.StatusForbidden)
					return
				}

				w.Header().Set("Access-Control-Allow-Methods", allowMethodsHeader)
				w.Header().Set("Access-Control-Allow-Headers", allowHeadersHeader)
				if opts.PreflightMaxAge > 0 {
					w.Header().Set("Access-Control-Max-Age", strconv.Itoa(opts.PreflightMaxAge))
				}

				w.WriteHeader(http.StatusNoContent)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

func resolveAllowedOrigin(origin string, allowedOrigins []string, allowAllOrigins bool, allowCredentials bool) string {
	if origin == "" {
		return ""
	}

	if allowAllOrigins {
		if allowCredentials {
			return origin
		}
		return "*"
	}

	for _, candidate := range allowedOrigins {
		if strings.EqualFold(strings.TrimSpace(candidate), origin) {
			return origin
		}
	}

	return ""
}

func withDefault(values []string, fallback []string) []string {
	trimmed := make([]string, 0, len(values))
	for _, value := range values {
		v := strings.TrimSpace(value)
		if v != "" {
			trimmed = append(trimmed, v)
		}
	}
	if len(trimmed) == 0 {
		return fallback
	}
	return trimmed
}

func containsIgnoreCase(values []string, target string) bool {
	for _, value := range values {
		if strings.EqualFold(strings.TrimSpace(value), target) {
			return true
		}
	}
	return false
}
