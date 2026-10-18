import { z } from "zod";

// Client-side validation that mirrors the server's identity rules
// (docs/frontend/revision2.md §7.3), plus the password generator (§5.10).

export const USERNAME_RULES = "Use 3–32 characters: letters, digits, dot, dash, underscore.";

/** §5.10: 3–32 of `a-z 0-9 . _ -` after trimming and lowercasing. */
export const usernameSchema = z
  .string()
  .trim()
  .toLowerCase()
  .min(3, USERNAME_RULES)
  .max(32, USERNAME_RULES)
  .regex(/^[a-z0-9._-]+$/, USERNAME_RULES);

/** §7.3: at least `passwordMinLength` characters and at most 1024 UTF-8 bytes. */
export function newPasswordSchema(minLength: number) {
  return z
    .string()
    .min(minLength, `Use at least ${minLength} characters.`)
    .refine((value) => new TextEncoder().encode(value).length <= 1024, "That's too long.");
}

export const OIDC_ISSUER_MESSAGE = "The issuer must be an https URL.";

export const oidcIssuerSchema = z
  .string()
  .trim()
  .refine((value) => {
    try {
      return new URL(value).protocol === "https:";
    } catch {
      return false;
    }
  }, OIDC_ISSUER_MESSAGE);

export const oidcSubjectSchema = z.string().trim().min(1, "The subject must not be empty.");

// An unambiguous alphabet (no 0/O, 1/l/I) for generated passwords (§5.10).
const PASSWORD_ALPHABET = "ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz23456789";

/** 20 random characters from an unambiguous alphabet, via `crypto.getRandomValues`. */
export function generatePassword(): string {
  const bytes = new Uint32Array(20);
  crypto.getRandomValues(bytes);
  let result = "";
  for (const value of bytes) {
    result += PASSWORD_ALPHABET[value % PASSWORD_ALPHABET.length];
  }
  return result;
}
