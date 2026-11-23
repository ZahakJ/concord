package app.concord.mobile;

import android.content.Context;
import android.content.SharedPreferences;
import android.content.pm.PackageManager;
import android.os.Build;
import android.security.keystore.KeyGenParameterSpec;
import android.security.keystore.KeyInfo;
import android.security.keystore.KeyPermanentlyInvalidatedException;
import android.security.keystore.KeyProperties;
import android.security.keystore.StrongBoxUnavailableException;

import androidx.annotation.NonNull;
import androidx.biometric.BiometricManager;
import androidx.biometric.BiometricPrompt;
import androidx.core.content.ContextCompat;
import androidx.fragment.app.FragmentActivity;

import java.io.ByteArrayOutputStream;
import java.io.File;
import java.io.FileOutputStream;
import java.nio.charset.StandardCharsets;
import java.nio.file.Files;
import java.security.KeyStore;
import java.security.SecureRandom;
import java.util.Arrays;
import java.util.Collections;

import javax.crypto.AEADBadTagException;
import javax.crypto.Cipher;
import javax.crypto.KeyGenerator;
import javax.crypto.SecretKey;
import javax.crypto.SecretKeyFactory;
import javax.crypto.spec.GCMParameterSpec;

import concord.Node;

/**
 * Fingerprint unlock (DESIGN-BIOUNLOCK, revision 2).
 *
 * U, 32 random bytes, opens the keystore's device slot in the Go core. Its
 * only copy at rest is noBackupFilesDir/bio-unlock.bin, encrypted by an
 * AES-GCM key in the Android Keystore that a Class 3 (strong) biometric must
 * release for every use. U goes from here to the core in process
 * (Node.useDeviceSecret / enrolDeviceSecret) and never to the WebView: the
 * page only learns whether it worked.
 *
 * The enrolment is deleted only on proof that it is dead: the key was
 * invalidated (fingerprints changed, screen lock removed), the alias is gone,
 * the GCM tag fails, or the core says U does not open the slot. Lockout,
 * cancel and other Keystore errors fall back to the passphrase and keep it.
 */
final class BioUnlock {
    static final String ALIAS_PREFIX = "concord.bio.v1.";
    static final int SECRET_SIZE = 32;
    static final int ID_SIZE = 16;
    private static final String FILE = "bio-unlock.bin";
    private static final byte FILE_VERSION = 1;
    private static final String AAD = "concord-bio-unlock-v1";
    private static final String KEYSTORE = "AndroidKeyStore";
    private static final int AUTHENTICATORS = BiometricManager.Authenticators.BIOMETRIC_STRONG;

    /** Outcome codes the page sees (never U, never key material). */
    static final String OK = "ok";
    static final String CANCELLED = "cancelled";
    static final String LOCKOUT = "lockout";
    static final String INVALIDATED = "invalidated";
    static final String NO_SLOT = "no_slot";
    static final String UNAVAILABLE = "unavailable";
    static final String FAILED = "failed";

    interface Done {
        void done(String code, String detail);
    }

    private BioUnlock() {}

    // ---- the wrapped U on disk -------------------------------------------

    static final class Wrapper {
        final byte[] id;
        final byte[] iv;
        final byte[] ct;

        Wrapper(byte[] id, byte[] iv, byte[] ct) {
            this.id = id;
            this.iv = iv;
            this.ct = ct;
        }
    }

    /** version(1) id(16) ivLen(1) iv ct — parsed strictly; anything else is null. */
    static Wrapper parse(byte[] b) {
        if (b == null || b.length < 2 + ID_SIZE || b[0] != FILE_VERSION) return null;
        int ivLen = b[1 + ID_SIZE] & 0xff;
        int off = 2 + ID_SIZE;
        if (ivLen < 12 || ivLen > 16 || b.length <= off + ivLen + 16) return null;
        return new Wrapper(
                Arrays.copyOfRange(b, 1, 1 + ID_SIZE),
                Arrays.copyOfRange(b, off, off + ivLen),
                Arrays.copyOfRange(b, off + ivLen, b.length));
    }

    static byte[] format(Wrapper w) {
        ByteArrayOutputStream out = new ByteArrayOutputStream();
        out.write(FILE_VERSION);
        out.write(w.id, 0, w.id.length);
        out.write(w.iv.length);
        out.write(w.iv, 0, w.iv.length);
        out.write(w.ct, 0, w.ct.length);
        return out.toByteArray();
    }

    static byte[] aad(byte[] id) {
        byte[] a = AAD.getBytes(StandardCharsets.US_ASCII);
        byte[] r = Arrays.copyOf(a, a.length + id.length);
        System.arraycopy(id, 0, r, a.length, id.length);
        return r;
    }

    static String alias(byte[] id) {
        StringBuilder sb = new StringBuilder(ALIAS_PREFIX);
        for (byte x : id) sb.append(String.format("%02x", x & 0xff));
        return sb.toString();
    }

    private static File file(Context ctx) {
        return new File(ctx.getNoBackupFilesDir(), FILE);
    }

    static Wrapper read(Context ctx) {
        try {
            File f = file(ctx);
            if (!f.exists()) return null;
            return parse(Files.readAllBytes(f.toPath()));
        } catch (Exception e) {
            return null;
        }
    }

    private static void write(Context ctx, Wrapper w) throws Exception {
        File f = file(ctx);
        File tmp = new File(f.getParentFile(), FILE + ".tmp");
        try (FileOutputStream out = new FileOutputStream(tmp)) {
            out.write(format(w));
            out.getFD().sync();
        }
        if (!tmp.renameTo(f)) throw new java.io.IOException("rename");
    }

    // ---- availability and state ------------------------------------------

    /** A strong biometric is enrolled and usable on this phone. */
    static boolean available(Context ctx) {
        return BiometricManager.from(ctx).canAuthenticate(AUTHENTICATORS) == BiometricManager.BIOMETRIC_SUCCESS;
    }

    /**
     * Reconciles the two halves at the lock screen. Returns true when the
     * phone's wrapper and the keystore's slot are the same enrolment. A
     * wrapper the slot does not match is deleted here (the native half); the
     * core is told which wrapper there is, and drops a slot without one at
     * the next passphrase unlock (it never writes the keystore while locked).
     * Stale aliases are swept.
     */
    static boolean reconcile(Context ctx, Node node) {
        Wrapper w = read(ctx);
        byte[] slot = node == null ? null : node.deviceSlotID();
        boolean paired = w != null && slot != null && slot.length > 0 && Arrays.equals(w.id, slot);
        if (w != null && !paired && node != null) {
            deleteLocal(ctx, w.id);
            w = null;
        }
        sweepAliases(w == null ? null : alias(w.id));
        if (node != null) node.setDeviceWrapper(w == null ? new byte[0] : w.id);
        return paired && available(ctx);
    }

    static void deleteLocal(Context ctx, byte[] id) {
        //noinspection ResultOfMethodCallIgnored
        file(ctx).delete();
        if (id != null) {
            try {
                KeyStore ks = KeyStore.getInstance(KEYSTORE);
                ks.load(null);
                ks.deleteEntry(alias(id));
            } catch (Exception ignored) {
            }
        }
    }

    private static void sweepAliases(String keep) {
        try {
            KeyStore ks = KeyStore.getInstance(KEYSTORE);
            ks.load(null);
            for (String a : Collections.list(ks.aliases())) {
                if (a.startsWith(ALIAS_PREFIX) && !a.equals(keep)) ks.deleteEntry(a);
            }
        } catch (Exception ignored) {
        }
    }

    // ---- the key -----------------------------------------------------------

    private static SecretKey generate(Context ctx, String alias, boolean strongBox) throws Exception {
        KeyGenParameterSpec.Builder b = new KeyGenParameterSpec.Builder(alias,
                KeyProperties.PURPOSE_ENCRYPT | KeyProperties.PURPOSE_DECRYPT)
                .setBlockModes(KeyProperties.BLOCK_MODE_GCM)
                .setEncryptionPaddings(KeyProperties.ENCRYPTION_PADDING_NONE)
                .setKeySize(256)
                .setUserAuthenticationRequired(true)
                .setInvalidatedByBiometricEnrollment(true);
        if (Build.VERSION.SDK_INT >= 30) {
            b.setUserAuthenticationParameters(0, KeyProperties.AUTH_BIOMETRIC_STRONG);
        } else {
            //noinspection deprecation
            b.setUserAuthenticationValidityDurationSeconds(-1);
        }
        if (strongBox && Build.VERSION.SDK_INT >= 28) b.setIsStrongBoxBacked(true);
        KeyGenerator g = KeyGenerator.getInstance(KeyProperties.KEY_ALGORITHM_AES, KEYSTORE);
        g.init(b.build());
        return g.generateKey();
    }

    /** Makes the key in StrongBox if the phone has one, else the TEE. */
    private static SecretKey newKey(Context ctx, String alias) throws Exception {
        boolean sb = Build.VERSION.SDK_INT >= 28
                && ctx.getPackageManager().hasSystemFeature(PackageManager.FEATURE_STRONGBOX_KEYSTORE);
        if (sb) {
            try {
                return generate(ctx, alias, true);
            } catch (StrongBoxUnavailableException e) {
                // fall through to the TEE
            }
        }
        return generate(ctx, alias, false);
    }

    /**
     * The key must live in secure hardware and its authentication be enforced
     * there. Debug builds accept the emulator's software keystore; a release
     * build has no such path (BuildConfig.DEBUG is a compile-time false).
     */
    static boolean hardwareBacked(SecretKey key) {
        if (BuildConfig.DEBUG) return true;
        try {
            SecretKeyFactory f = SecretKeyFactory.getInstance(key.getAlgorithm(), KEYSTORE);
            KeyInfo info = (KeyInfo) f.getKeySpec(key, KeyInfo.class);
            if (!info.isUserAuthenticationRequirementEnforcedBySecureHardware()) return false;
            if (Build.VERSION.SDK_INT >= 31) {
                return info.getSecurityLevel() != KeyProperties.SECURITY_LEVEL_SOFTWARE
                        && info.getSecurityLevel() != KeyProperties.SECURITY_LEVEL_UNKNOWN;
            }
            //noinspection deprecation
            return info.isInsideSecureHardware();
        } catch (Exception e) {
            return false;
        }
    }

    private static Cipher cipher() throws Exception {
        return Cipher.getInstance("AES/GCM/NoPadding");
    }

    private static BiometricPrompt.PromptInfo prompt(Context ctx, boolean enrol) {
        // Fixed strings: never taken from the page.
        return new BiometricPrompt.PromptInfo.Builder()
                .setTitle(ctx.getString(enrol ? R.string.bio_enrol_title : R.string.bio_unlock_title))
                .setSubtitle(ctx.getString(enrol ? R.string.bio_enrol_subtitle : R.string.bio_unlock_subtitle))
                .setNegativeButtonText(ctx.getString(R.string.bio_use_passphrase))
                .setAllowedAuthenticators(AUTHENTICATORS)
                .setConfirmationRequired(true)
                .build();
    }

    private static String errorCode(int err) {
        switch (err) {
            case BiometricPrompt.ERROR_LOCKOUT:
            case BiometricPrompt.ERROR_LOCKOUT_PERMANENT:
                return LOCKOUT;
            case BiometricPrompt.ERROR_USER_CANCELED:
            case BiometricPrompt.ERROR_NEGATIVE_BUTTON:
            case BiometricPrompt.ERROR_CANCELED:
                return CANCELLED;
            case BiometricPrompt.ERROR_NO_BIOMETRICS:
            case BiometricPrompt.ERROR_HW_NOT_PRESENT:
            case BiometricPrompt.ERROR_HW_UNAVAILABLE:
                return UNAVAILABLE;
            default:
                return FAILED;
        }
    }

    // ---- enrol ---------------------------------------------------------------

    /**
     * Turns fingerprint unlock on. passphrase is the one typed in Settings, or
     * "" right after a passphrase unlock (the core holds that proof for a
     * minute, once). Must run on the main thread.
     */
    static void enrol(FragmentActivity act, Node node, String passphrase, Done done) {
        Context ctx = act.getApplicationContext();
        if (!available(ctx)) {
            done.done(UNAVAILABLE, "");
            return;
        }
        SecureRandom rnd = new SecureRandom();
        byte[] id = new byte[ID_SIZE];
        rnd.nextBytes(id);
        String alias = alias(id);
        Cipher c;
        try {
            SecretKey key = newKey(ctx, alias);
            if (!hardwareBacked(key)) {
                deleteLocal(ctx, id);
                done.done(UNAVAILABLE, "software");
                return;
            }
            c = cipher();
            c.init(Cipher.ENCRYPT_MODE, key); // the Keystore picks the IV
        } catch (Exception e) {
            deleteLocal(ctx, id);
            done.done(FAILED, "");
            return;
        }
        BiometricPrompt bp = new BiometricPrompt(act, ContextCompat.getMainExecutor(ctx),
                new BiometricPrompt.AuthenticationCallback() {
                    @Override
                    public void onAuthenticationSucceeded(@NonNull BiometricPrompt.AuthenticationResult r) {
                        byte[] u = new byte[SECRET_SIZE];
                        try {
                            BiometricPrompt.CryptoObject co = r.getCryptoObject();
                            Cipher ac = co == null ? null : co.getCipher();
                            if (ac == null) throw new IllegalStateException("no cipher");
                            rnd.nextBytes(u);
                            ac.updateAAD(aad(id));
                            byte[] ct = ac.doFinal(u);
                            byte[] iv = ac.getIV();
                            try {
                                // Slot first, then the wrapper: a crash between
                                // leaves a slot nothing can open, which the next
                                // passphrase unlock drops.
                                node.enrolDeviceSecret(id, u, passphrase);
                            } catch (Exception e) {
                                deleteLocal(ctx, id);
                                done.done(FAILED, e.getMessage() == null ? "" : e.getMessage());
                                return;
                            }
                            try {
                                write(ctx, new Wrapper(id, iv, ct));
                            } catch (Exception e) {
                                deleteLocal(ctx, id);
                                node.setDeviceWrapper(new byte[0]); // the core drops the slot
                                done.done(FAILED, "");
                                return;
                            }
                            node.setDeviceWrapper(id);
                            sweepAliases(alias);
                            done.done(OK, "");
                        } catch (Exception e) {
                            deleteLocal(ctx, id);
                            done.done(FAILED, "");
                        } finally {
                            Arrays.fill(u, (byte) 0);
                        }
                    }

                    @Override
                    public void onAuthenticationError(int err, @NonNull CharSequence msg) {
                        deleteLocal(ctx, id);
                        done.done(errorCode(err), "");
                    }
                });
        bp.authenticate(prompt(ctx, true), new BiometricPrompt.CryptoObject(c));
    }

    // ---- unlock (and the App Lock gate) ----------------------------------------

    /**
     * Unlocks with the fingerprint, or opens the App Lock gate when a session
     * is already open. Must run on the main thread.
     */
    static void unlock(FragmentActivity act, Node node, Done done) {
        Context ctx = act.getApplicationContext();
        Wrapper w = read(ctx);
        byte[] slot = node.deviceSlotID();
        if (w == null || slot == null || !Arrays.equals(w.id, slot)) {
            reconcile(ctx, node);
            done.done(NO_SLOT, "");
            return;
        }
        Cipher c;
        try {
            KeyStore ks = KeyStore.getInstance(KEYSTORE);
            ks.load(null);
            SecretKey key = (SecretKey) ks.getKey(alias(w.id), null);
            if (key == null) {
                dead(ctx, node, w);
                done.done(INVALIDATED, "");
                return;
            }
            c = cipher();
            c.init(Cipher.DECRYPT_MODE, key, new GCMParameterSpec(128, w.iv));
        } catch (KeyPermanentlyInvalidatedException e) {
            dead(ctx, node, w);
            done.done(INVALIDATED, "");
            return;
        } catch (Exception e) {
            // A Keystore or provider error is not proof the enrolment is dead.
            done.done(FAILED, "");
            return;
        }
        BiometricPrompt bp = new BiometricPrompt(act, ContextCompat.getMainExecutor(ctx),
                new BiometricPrompt.AuthenticationCallback() {
                    @Override
                    public void onAuthenticationSucceeded(@NonNull BiometricPrompt.AuthenticationResult r) {
                        byte[] u = null;
                        try {
                            BiometricPrompt.CryptoObject co = r.getCryptoObject();
                            Cipher ac = co == null ? null : co.getCipher();
                            if (ac == null) throw new IllegalStateException("no cipher");
                            ac.updateAAD(aad(w.id));
                            u = ac.doFinal(w.ct);
                        } catch (AEADBadTagException e) {
                            dead(ctx, node, w);
                            done.done(INVALIDATED, "");
                            return;
                        } catch (Exception e) {
                            done.done(FAILED, "");
                            return;
                        }
                        try {
                            node.useDeviceSecret(w.id, u);
                            done.done(OK, "");
                        } catch (Exception e) {
                            String m = e.getMessage() == null ? "" : e.getMessage();
                            if (m.contains("fingerprint unlock does not open this keystore")) {
                                // U unwrapped fine and still does not open the
                                // slot: this enrolment is dead.
                                dead(ctx, node, w);
                                done.done(INVALIDATED, "");
                            } else {
                                done.done(FAILED, m);
                            }
                        } finally {
                            Arrays.fill(u, (byte) 0);
                        }
                    }

                    @Override
                    public void onAuthenticationError(int err, @NonNull CharSequence msg) {
                        done.done(errorCode(err), "");
                    }
                });
        bp.authenticate(prompt(ctx, false), new BiometricPrompt.CryptoObject(c));
    }

    private static void dead(Context ctx, Node node, Wrapper w) {
        deleteLocal(ctx, w.id);
        node.setDeviceWrapper(new byte[0]);
    }

    /** Turns fingerprint unlock off: the slot (needs an open, ungated session), then the phone's half. */
    static void disable(Context ctx, Node node) throws Exception {
        node.dropDeviceSlot();
        Wrapper w = read(ctx);
        deleteLocal(ctx, w == null ? null : w.id);
        sweepAliases(null);
        node.setDeviceWrapper(new byte[0]);
    }

    // ---- the 0.63 plugin's leftovers ---------------------------------------------

    private static final String SWEPT = "legacyBioSwept";
    static final String LEGACY_PREFS = "NativeBiometricSharedPreferences";
    static final String LEGACY_ALIAS = "app.concord.mobile";

    /**
     * capacitor-native-biometric (0.63 and earlier) kept the passphrase itself
     * in SharedPreferences under a key no biometric guarded, readable by any
     * script in the page. Its data is deleted on the first 0.64 start, before
     * the WebView loads; users turn fingerprint unlock on again.
     */
    static void sweepLegacy(Context ctx) {
        SharedPreferences own = ctx.getSharedPreferences("concord.bio", Context.MODE_PRIVATE);
        if (own.getBoolean(SWEPT, false)) return;
        try {
            ctx.getSharedPreferences(LEGACY_PREFS, Context.MODE_PRIVATE).edit().clear().commit();
            ctx.deleteSharedPreferences(LEGACY_PREFS);
            // Its fallback wrapping key for old Android versions.
            ctx.getSharedPreferences("", Context.MODE_PRIVATE).edit().remove("NativeBiometricKey").commit();
        } catch (Exception ignored) {
        }
        try {
            KeyStore ks = KeyStore.getInstance(KEYSTORE);
            ks.load(null);
            if (ks.containsAlias(LEGACY_ALIAS)) ks.deleteEntry(LEGACY_ALIAS);
        } catch (Exception ignored) {
        }
        own.edit().putBoolean(SWEPT, true).commit();
    }
}
