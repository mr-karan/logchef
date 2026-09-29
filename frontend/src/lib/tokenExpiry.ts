import { formatDate } from "@/utils/format";

export type BadgeVariant = "default" | "destructive" | "success" | "outline" | "secondary";

export type ExpiryStatus =
    | { kind: "never"; variant: BadgeVariant; isExpired: false }
    | { kind: "expired"; date: string; variant: BadgeVariant; isExpired: true }
    | { kind: "expiringSoon"; days: number; variant: BadgeVariant; isExpired: false }
    | { kind: "expires"; date: string; variant: BadgeVariant; isExpired: false };

export function isTokenExpired(expiresAt: string | null | undefined): boolean {
    if (!expiresAt) return false;
    return new Date(expiresAt) < new Date();
}

// Shared by the profile API tokens list and the service tokens admin page so
// both surface expiry identically (never expires / expires / expiring soon / expired).
export function getExpiryStatus(expiresAt: string | null | undefined): ExpiryStatus {
    if (!expiresAt) return { kind: "never", variant: "secondary", isExpired: false };

    const expiry = new Date(expiresAt);
    const now = new Date();
    const isExpired = expiry < now;

    if (isExpired) {
        return { kind: "expired", date: formatDate(expiresAt), variant: "destructive", isExpired: true };
    }

    // Check if expiring soon (within 7 days)
    const daysUntilExpiry = Math.ceil((expiry.getTime() - now.getTime()) / (1000 * 60 * 60 * 24));
    if (daysUntilExpiry <= 7) {
        return { kind: "expiringSoon", days: daysUntilExpiry, variant: "outline", isExpired: false };
    }

    return { kind: "expires", date: formatDate(expiresAt), variant: "secondary", isExpired: false };
}
