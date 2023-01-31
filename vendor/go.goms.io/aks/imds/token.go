package imds

import (
	"encoding/json"
	"fmt"
	"strconv"
	"time"
)

const (
	keyAccessToken  = "access_token"
	keyClientID     = "client_id"
	keyRefreshToken = "refresh_token"
	keyResource     = "resource"
	keyTokenType    = "token_type"
	keyExpiresIn    = "expires_in"
	keyExpiresOn    = "expires_on"
	keyNotBefore    = "not_before"
)

type TokenRequestOptions struct {
	ManagedIdentityObjectID   string
	ManagedIdentityClientID   string
	ManagedIdentityResourceID string
}

type Token struct {
	AccessToken  string
	RefreshToken string
	ClientID     string
	ExpiresIn    int
	ExpiresOn    time.Time
	NotBefore    time.Time
	Resource     string
	TokenType    string
}

//nolint:cyclop
func (t *Token) UnmarshalJSON(b []byte) error {
	var err error

	m := make(map[string]interface{})

	if err = json.Unmarshal(b, &m); err != nil {
		return err
	}

	accessToken, ok := m[keyAccessToken]
	if ok {
		if t.AccessToken, ok = accessToken.(string); !ok {
			return fmt.Errorf("failed to convert %q to string", keyAccessToken)
		}
	}

	refreshToken, ok := m[keyRefreshToken]
	if ok {
		if t.RefreshToken, ok = refreshToken.(string); !ok {
			return fmt.Errorf("failed to convert %q to string", keyRefreshToken)
		}
	}

	clientID, ok := m[keyClientID]
	if ok {
		if t.ClientID, ok = clientID.(string); !ok {
			return fmt.Errorf("failed to convert %q to string", keyClientID)
		}
	}

	resource, ok := m[keyResource]
	if ok {
		if t.Resource, ok = resource.(string); !ok {
			return fmt.Errorf("failed to convert %q to string", keyResource)
		}
	}

	tokenType, ok := m[keyTokenType]
	if ok {
		if t.TokenType, ok = tokenType.(string); !ok {
			return fmt.Errorf("failed to convert %q to string", keyTokenType)
		}
	}

	expiresIn, ok := m[keyExpiresIn]
	if ok {
		if t.ExpiresIn, err = strconv.Atoi(expiresIn.(string)); err != nil {
			return err
		}
	}

	expiresOnSeconds, ok := m[keyExpiresOn]
	if ok {
		expiresOn, err := strconv.Atoi(expiresOnSeconds.(string))
		if err != nil {
			return err
		}

		t.ExpiresOn = time.Unix(int64(expiresOn), 0).UTC()
	}

	notBeforeSeconds, ok := m[keyNotBefore]
	if ok {
		notBefore, err := strconv.Atoi(notBeforeSeconds.(string))
		if err != nil {
			return err
		}

		t.NotBefore = time.Unix(int64(notBefore), 0).UTC()
	}

	return nil
}
