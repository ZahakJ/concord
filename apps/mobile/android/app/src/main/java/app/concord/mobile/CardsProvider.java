package app.concord.mobile;

import android.content.ContentProvider;
import android.content.ContentValues;
import android.content.Context;
import android.content.pm.PackageManager;
import android.database.Cursor;
import android.database.MatrixCursor;
import android.net.Uri;
import android.os.Build;
import android.os.ParcelFileDescriptor;

/**
 * Concord's cards for the owner's Jayb (jaybos CONTRACTS.md §2, §3): one
 * read-only row of numbers at content://app.concord.mobile.cards/unread.
 *
 * Two locks, both required. The manifest puts the provider behind
 * app.concord.mobile.permission.READ_CARDS, signature|knownSigner, which
 * Android grants only to an app signed by a key in R.array.card_readers.
 * And every query checks the caller's signer here against the same list, with
 * the shared debug key refused outside a debug build. Selection, arguments and
 * sort are ignored; every write throws; an unknown path is null.
 */
public class CardsProvider extends ContentProvider {
    static final String[] COLUMNS = {"total", "conversations", "asOf", "partial"};

    @Override
    public boolean onCreate() {
        return true;
    }

    @Override
    public Cursor query(Uri uri, String[] projection, String selection, String[] selectionArgs, String sortOrder) {
        Context ctx = getContext();
        if (ctx == null) return null;
        if (!(ctx.getPackageName() + UnreadCard.AUTHORITY_SUFFIX).equals(uri.getAuthority())) return null;
        if (uri.getPathSegments().size() != 1 || !"unread".equals(uri.getPathSegments().get(0))) return null;
        if (!callerTrusted(ctx)) throw new SecurityException("Concord's cards are for the owner's Jayb only");
        UnreadSummary u = UnreadCard.read(ctx);
        MatrixCursor c = new MatrixCursor(COLUMNS, 1);
        c.addRow(new Object[] {u.total(), (long) u.conversations(), u.asOf(), u.partial() ? 1L : 0L});
        return c;
    }

    private boolean callerTrusted(Context ctx) {
        // hasSigningCertificate needs Android 9; knownSigner itself needs 12, so older callers hold no grant anyway.
        if (Build.VERSION.SDK_INT < Build.VERSION_CODES.P) return false;
        final PackageManager pm = ctx.getPackageManager();
        return CardTrust.allowed(getCallingPackage(), ctx.getResources().getStringArray(R.array.card_readers), BuildConfig.DEBUG,
            (pkg, d) -> {
                try {
                    return pm.hasSigningCertificate(pkg, d, PackageManager.CERT_INPUT_SHA256);
                } catch (RuntimeException e) {
                    return false;
                }
            });
    }

    @Override
    public String getType(Uri uri) {
        return null;
    }

    @Override
    public Uri insert(Uri uri, ContentValues values) {
        throw new UnsupportedOperationException("read-only");
    }

    @Override
    public int update(Uri uri, ContentValues values, String selection, String[] selectionArgs) {
        throw new UnsupportedOperationException("read-only");
    }

    @Override
    public int delete(Uri uri, String selection, String[] selectionArgs) {
        throw new UnsupportedOperationException("read-only");
    }

    @Override
    public ParcelFileDescriptor openFile(Uri uri, String mode) {
        throw new UnsupportedOperationException("read-only");
    }
}
