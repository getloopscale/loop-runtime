// Google OIDC data mapper for Ory Kratos.
// Maps Google OpenID Connect claims to Kratos identity traits.
//
// Google ID token claims:
//   sub: "1234567890"
//   email: "user@gmail.com"
//   email_verified: true
//   name: "User Name"
//
// Kratos identity schema expects:
//   traits.email: required, used as login identifier
//   traits.name: optional

local claims = std.extVar('claims');

{
  identity: {
    traits: {
      email: claims.email,
      [if std.objectHas(claims, 'name') && claims.name != null then 'name']: claims.name,
    },
  },
}
