// GitHub OIDC data mapper for Ory Kratos.
// Maps GitHub user data to Kratos identity traits.
//
// GitHub /user response fields:
//   login: "octocat"
//   id: 1234567
//   email: "octocat@github.com" (may be null if private)
//   name: "The Octocat"
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
