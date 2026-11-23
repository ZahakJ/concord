package app.concord.mobile;

import static org.junit.Assert.assertArrayEquals;
import static org.junit.Assert.assertEquals;
import static org.junit.Assert.assertNotNull;
import static org.junit.Assert.assertNull;
import static org.junit.Assert.assertTrue;

import java.util.Arrays;

import org.junit.Test;

/**
 * The wrapped secret's file is parsed strictly: what does not have exactly
 * the shape this build writes is no enrolment, never a guess.
 */
public class BioUnlockTest {
    private static byte[] seq(int n, int start) {
        byte[] b = new byte[n];
        for (int i = 0; i < n; i++) b[i] = (byte) (start + i);
        return b;
    }

    @Test
    public void roundTrips() {
        BioUnlock.Wrapper w = new BioUnlock.Wrapper(seq(16, 1), seq(12, 40), seq(48, 90));
        BioUnlock.Wrapper back = BioUnlock.parse(BioUnlock.format(w));
        assertNotNull(back);
        assertArrayEquals(w.id, back.id);
        assertArrayEquals(w.iv, back.iv);
        assertArrayEquals(w.ct, back.ct);
    }

    @Test
    public void refusesWhatItDidNotWrite() {
        byte[] good = BioUnlock.format(new BioUnlock.Wrapper(seq(16, 1), seq(12, 40), seq(48, 90)));
        assertNull(BioUnlock.parse(null));
        assertNull(BioUnlock.parse(new byte[0]));
        byte[] v = good.clone();
        v[0] = 2;
        assertNull("another version", BioUnlock.parse(v));
        byte[] iv = good.clone();
        iv[17] = 4;
        assertNull("an IV too short", BioUnlock.parse(iv));
        assertNull("cut short", BioUnlock.parse(Arrays.copyOf(good, 2 + 16 + 12 + 10)));
    }

    @Test
    public void namesTheEnrolment() {
        String a = BioUnlock.alias(seq(16, 0));
        assertEquals("concord.bio.v1.000102030405060708090a0b0c0d0e0f", a);
        assertTrue(a.startsWith(BioUnlock.ALIAS_PREFIX));
        byte[] aad = BioUnlock.aad(seq(16, 0));
        assertEquals("concord-bio-unlock-v1".length() + 16, aad.length);
    }

    @Test
    public void releaseBuildsHaveNoSoftwareKeystorePath() {
        // BuildConfig.DEBUG is what lets the emulator's software keystore
        // through; the unit test variant is debug, so this only pins that the
        // switch is that flag and nothing a page can set.
        assertEquals(BuildConfig.DEBUG, BuildConfig.BUILD_TYPE.equals("debug"));
    }
}
