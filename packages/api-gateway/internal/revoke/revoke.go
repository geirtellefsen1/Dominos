// Package revoke implements the one-revoke primitive from spec §3.4.
//
// When a human or AI identity is revoked we must atomically:
//
//  1. mark the identity row inactive (callers do this via scim.Store
//     or agents.Store — revoke doesn't own the row),
//  2. invalidate in-flight authenticators (sessions for users, certs
//     for agents — also the caller's responsibility),
//  3. strip every ACL tuple where the identity is the SUBJECT so the
//     next authenticated call by anyone CANNOT land back on a resource
//     the revoked identity was authorised for.
//
// This package owns step 3. Steps 1 and 2 live in the identity stores
// so that revocation works even when FGA is unreachable, but an FGA
// cleanup failure here is surfaced as an error so the admin knows the
// principal is still wired into the ACL graph.
package revoke

import (
	"context"
	"errors"
	"fmt"

	"github.com/geirtellefsen1/dominos/packages/api-gateway/internal/acl"
)

// Principal is the "<kind>:<id>" form used throughout the codebase.
type Principal string

// SubjectTuples removes every tuple where `principal` is the subject
// across the document type's three relations (owner, reader, writer).
// Returns (countRemoved, err).
//
// OpenFGA has no "delete all tuples for subject" call, so we enumerate
// via ListObjects per relation and issue bulk Delete writes.
func SubjectTuples(ctx context.Context, fga *acl.Client, principal Principal) (int, error) {
	if fga == nil {
		return 0, errors.New("fga client not configured; cannot revoke acl tuples")
	}
	if principal == "" {
		return 0, errors.New("empty principal")
	}

	total := 0
	for _, rel := range []string{acl.RelOwner, acl.RelReader, acl.RelWriter} {
		objects, err := fga.ListObjects(ctx, string(principal), rel, acl.TypeDocument)
		if err != nil {
			return total, fmt.Errorf("list objects (%s): %w", rel, err)
		}
		if len(objects) == 0 {
			continue
		}
		tuples := make([]acl.Tuple, 0, len(objects))
		for _, obj := range objects {
			tuples = append(tuples, acl.Tuple{
				User:     string(principal),
				Relation: rel,
				Object:   obj,
			})
		}
		// FGA writes are capped at 100 tuples per call; chunk to be safe.
		for i := 0; i < len(tuples); i += 100 {
			end := i + 100
			if end > len(tuples) {
				end = len(tuples)
			}
			if err := fga.Delete(ctx, tuples[i:end]...); err != nil {
				return total, fmt.Errorf("delete batch (%s): %w", rel, err)
			}
			total += end - i
		}
	}
	return total, nil
}
