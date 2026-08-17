package httpapi

import "mcmods-cn-backend/internal/security"

func projectMutationBypassesReview(claims security.Claims) bool {
	return claimsAllow(claims, "admin.*") || claimsAllow(claims, "project.no-review") || claimsAllow(claims, "content.no-review")
}
