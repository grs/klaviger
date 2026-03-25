#!/bin/bash -e

KC_POD=$(kubectl get pods | grep keycloak | cut -f 1 -d ' ')
KCADMIN="kubectl exec $KC_POD -- /opt/keycloak/bin/kcadm.sh"

$KCADMIN config credentials --server http://localhost:8080 --realm master --user admin --password admin

$KCADMIN create realms -s realm=demo -s enabled=true -s verifyEmail=false

$KCADMIN create identity-provider/instances -r demo -s alias=kubernetes -s providerId=kubernetes -s config='{"issuer": "https://kubernetes.default.svc.cluster.local"}'

ALPHA_SCOPE_ID=$($KCADMIN create client-scopes -r demo -s name="alpha" -s protocol="openid-connect" -i)
BETA_SCOPE_ID=$($KCADMIN create client-scopes -r demo -s name="beta" -s protocol="openid-connect" -i)
GAMMA_SCOPE_ID=$($KCADMIN create client-scopes -r demo -s name="gamma" -s protocol="openid-connect" -i)
DELTA_SCOPE_ID=$($KCADMIN create client-scopes -r demo -s name="delta" -s protocol="openid-connect" -i)

$KCADMIN create client-scopes/$ALPHA_SCOPE_ID/protocol-mappers/models -r demo -s name="alpha-audience-mapper" -s protocol="openid-connect" -s protocolMapper="oidc-audience-mapper" -s config='{"included.client.audience":"alpha", "access.token.claim":"true"}'	
$KCADMIN create client-scopes/$BETA_SCOPE_ID/protocol-mappers/models -r demo -s name="beta-audience-mapper" -s protocol="openid-connect" -s protocolMapper="oidc-audience-mapper" -s config='{"included.client.audience":"beta", "access.token.claim":"true"}'	
$KCADMIN create client-scopes/$GAMMA_SCOPE_ID/protocol-mappers/models -r demo -s name="gamma-audience-mapper" -s protocol="openid-connect" -s protocolMapper="oidc-audience-mapper" -s config='{"included.client.audience":"gamma", "access.token.claim":"true"}'	
$KCADMIN create client-scopes/$DELTA_SCOPE_ID/protocol-mappers/models -r demo -s name="delta-audience-mapper" -s protocol="openid-connect" -s protocolMapper="oidc-audience-mapper" -s config='{"included.client.audience":"delta", "access.token.claim":"true"}'	

$KCADMIN create clients -r demo -s clientId=alpha -s serviceAccountsEnabled=true -s standardFlowEnabled=true -s clientAuthenticatorType=federated-jwt -s attributes='{ "jwt.credential.issuer": "kubernetes", "jwt.credential.sub": "system:serviceaccount:default:alpha", "standard.token.exchange.enabled": "true" }'
$KCADMIN create clients -r demo -s clientId=beta -s serviceAccountsEnabled=true -s standardFlowEnabled=true -s clientAuthenticatorType=federated-jwt -s attributes='{ "jwt.credential.issuer": "kubernetes", "jwt.credential.sub": "system:serviceaccount:default:beta", "standard.token.exchange.enabled": "true" }'
$KCADMIN create clients -r demo -s clientId=gamma -s serviceAccountsEnabled=true -s standardFlowEnabled=true -s clientAuthenticatorType=federated-jwt -s attributes='{ "jwt.credential.issuer": "kubernetes", "jwt.credential.sub": "system:serviceaccount:default:gamma", "standard.token.exchange.enabled": "true" }'
$KCADMIN create clients -r demo -s clientId=delta -s serviceAccountsEnabled=true -s standardFlowEnabled=true -s clientAuthenticatorType=federated-jwt -s attributes='{ "jwt.credential.issuer": "kubernetes", "jwt.credential.sub": "system:serviceaccount:default:delta", "standard.token.exchange.enabled": "true" }'

$KCADMIN create clients -r demo -s clientId=demo-client -s publicClient=true -s directAccessGrantsEnabled=true -s enabled=true
$KCADMIN create users -r demo -s username=demouser -s enabled=true -s firstName=Demo -s lastName=User -s "email=demouser@foo.com" -s emailVerified=true -s "requiredActions=[]"
$KCADMIN set-password -r demo --username demouser --new-password demopass

for scope in $ALPHA_SCOPE_ID $BETA_SCOPE_ID $GAMMA_SCOPE_ID $DELTA_SCOPE_ID ; do
    for client in alpha beta gamma delta demo-client; do
        CLIENT_ID=$($KCADMIN get clients -r demo -q clientId=$client --fields id --format csv --noquotes)
        $KCADMIN update clients/$CLIENT_ID/default-client-scopes/$scope -r demo
    done
done
