import type { DemoLoginCredentials } from "@/api/meta";

export interface LoginFields {
  email: string;
  password: string;
}

// Demo metadata can arrive after a visitor has begun typing. Fill each field
// independently so an empty password still gets populated without replacing
// an email (or password) the visitor already entered.
export function prefillEmptyLoginFields(
  fields: LoginFields,
  credentials: DemoLoginCredentials
): LoginFields {
  return {
    email: fields.email || credentials.email,
    password: fields.password || credentials.password,
  };
}
