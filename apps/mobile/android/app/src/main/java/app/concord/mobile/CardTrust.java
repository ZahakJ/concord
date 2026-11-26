package app.concord.mobile;

/**
 * Who may read Concord's cards (jaybos CONTRACTS.md §1): an app signed by one
 * of the listed certificates (R.array.card_readers, Jayb's release key; the
 * shared debug key only in a debug build). The platform enforces the same list
 * through the knownSigner permission; this is the provider's own check, kept
 * free of Android so it is unit tested.
 */
final class CardTrust {
    /** The shared Android debug key: eight apps carry it, so it proves nothing in a release. */
    static final String DEBUG_CERT = "5a6ef046fc93fe8d30b032a80188a5455556b3e6ae2d229adca9d5a6fdee8cb8";

    interface Signed {
        boolean by(String pkg, byte[] sha256);
    }

    private CardTrust() {}

    static boolean allowed(String caller, String[] readers, boolean debugBuild, Signed signed) {
        if (caller == null || caller.isEmpty() || readers == null) return false;
        for (String r : readers) {
            byte[] d = digest(r);
            if (d == null) continue;
            if (DEBUG_CERT.equalsIgnoreCase(r.trim()) && !debugBuild) continue;
            if (signed.by(caller, d)) return true;
        }
        return false;
    }

    /**
     * The caller's UID passes only when it has packages, [caller] is one of them, and every one of them is signed
     * by a listed reader (review D2: a shared UID can't lend one package's key to another).
     */
    static boolean allowedUid(String caller, String[] uidPackages, String[] readers, boolean debugBuild, Signed signed) {
        if (uidPackages == null || uidPackages.length == 0 || caller == null) return false;
        boolean named = false;
        for (String p : uidPackages) {
            if (caller.equals(p)) named = true;
            if (!allowed(p, readers, debugBuild, signed)) return false;
        }
        return named;
    }

    /** 64 hex characters to 32 bytes; anything else is null. */
    static byte[] digest(String hex) {
        if (hex == null) return null;
        String h = hex.trim();
        if (h.length() != 64) return null;
        byte[] out = new byte[32];
        for (int i = 0; i < 32; i++) {
            int hi = Character.digit(h.charAt(2 * i), 16), lo = Character.digit(h.charAt(2 * i + 1), 16);
            if (hi < 0 || lo < 0) return null;
            out[i] = (byte) ((hi << 4) | lo);
        }
        return out;
    }
}
