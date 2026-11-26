package app.concord.mobile;

import static org.junit.Assert.assertArrayEquals;
import static org.junit.Assert.assertEquals;
import static org.junit.Assert.assertFalse;
import static org.junit.Assert.assertNull;
import static org.junit.Assert.assertTrue;

import java.nio.charset.StandardCharsets;
import java.nio.file.Files;
import java.nio.file.Path;
import java.nio.file.Paths;
import java.util.ArrayList;
import java.util.Arrays;
import java.util.List;
import java.util.stream.Stream;

import org.junit.Test;

/** Who may read Concord's cards (jaybos CONTRACTS.md §1), and that a release lists no debug key. */
public class CardTrustTest {
    private static final String JAYB = "9024547629d08815f4003501036306f3bf44064200a6ad23b7fecf2b6c34210c";
    private static final String[] RELEASE = {JAYB};
    private static final String[] DEBUG = {JAYB, CardTrust.DEBUG_CERT};

    /** A fake PackageManager: the package signed by exactly this digest. */
    private static CardTrust.Signed signedBy(String pkg, String hex) {
        byte[] d = CardTrust.digest(hex);
        return (p, x) -> p.equals(pkg) && Arrays.equals(x, d);
    }

    @Test
    public void onlyAListedSignerReads() {
        assertTrue(CardTrust.allowed("space.avicenna.jayb", RELEASE, false, signedBy("space.avicenna.jayb", JAYB)));
        assertFalse("another key", CardTrust.allowed("space.avicenna.jayb", RELEASE, false, signedBy("space.avicenna.jayb", "ee".repeat(32))));
        assertFalse("no caller", CardTrust.allowed(null, RELEASE, false, signedBy("space.avicenna.jayb", JAYB)));
        assertFalse("no list", CardTrust.allowed("space.avicenna.jayb", null, false, signedBy("space.avicenna.jayb", JAYB)));
    }

    @Test
    public void theDebugKeyReadsOnlyInADebugBuild() {
        CardTrust.Signed dbg = signedBy("space.avicenna.jayb.debug", CardTrust.DEBUG_CERT);
        assertTrue(CardTrust.allowed("space.avicenna.jayb.debug", DEBUG, true, dbg));
        assertFalse("even listed, a release refuses it", CardTrust.allowed("space.avicenna.jayb.debug", DEBUG, false, dbg));
    }

    @Test
    public void everyPackageOfTheCallingUidMustBeAReader() {
        CardTrust.Signed jayb = signedBy("space.avicenna.jayb", JAYB);
        assertTrue(CardTrust.allowedUid("space.avicenna.jayb", new String[] {"space.avicenna.jayb"}, RELEASE, false, jayb));
        assertFalse("a stranger sharing the UID", CardTrust.allowedUid("space.avicenna.jayb", new String[] {"space.avicenna.jayb", "org.example.x"}, RELEASE, false, jayb));
        assertFalse("named a package not of the UID", CardTrust.allowedUid("space.avicenna.jayb", new String[] {"org.example.x"}, RELEASE, false, (p, d) -> true));
        assertFalse("no packages", CardTrust.allowedUid("space.avicenna.jayb", new String[0], RELEASE, false, jayb));
        assertFalse(CardTrust.allowedUid("space.avicenna.jayb", null, RELEASE, false, jayb));
    }

    @Test
    public void digestsAreStrict() {
        assertEquals(32, CardTrust.digest(JAYB).length);
        assertArrayEquals(CardTrust.digest(JAYB), CardTrust.digest(JAYB.toUpperCase()));
        assertNull(CardTrust.digest("90:24"));
        assertNull(CardTrust.digest(JAYB.substring(2) + "zz"));
        assertNull(CardTrust.digest(null));
        assertFalse("a malformed entry admits nobody", CardTrust.allowed("x", new String[] {"zz"}, true, (p, d) -> true));
    }

    @Test
    public void noResourceOutsideSrcDebugListsTheDebugKey() throws Exception {
        // The release check (scripts/check-card-readers.mjs does the same in CI): the debug digest only under src/debug.
        Path src = Paths.get("src");
        List<Path> bad = new ArrayList<>();
        boolean found = false;
        try (Stream<Path> s = Files.walk(src)) {
            for (Path p : (Iterable<Path>) s.filter(Files::isRegularFile).filter(x -> x.toString().endsWith(".xml"))::iterator) {
                String xml = new String(Files.readAllBytes(p), StandardCharsets.UTF_8).toLowerCase();
                if (!xml.contains(CardTrust.DEBUG_CERT)) continue;
                if (p.startsWith(Paths.get("src", "debug"))) found = true;
                else bad.add(p);
            }
        }
        assertTrue("outside src/debug: " + bad, bad.isEmpty());
        assertTrue("the debug build still lists it", found);
        String main = new String(Files.readAllBytes(Paths.get("src/main/res/values/card_readers.xml")), StandardCharsets.UTF_8);
        assertTrue("the release lists Jayb's key", main.contains(JAYB));
    }
}
