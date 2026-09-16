package dev.eswink.aicontrolhud;

import static org.junit.Assert.assertEquals;
import static org.junit.Assert.assertNotNull;
import static org.junit.Assert.assertNull;
import static org.junit.Assert.assertTrue;

import org.json.JSONArray;
import org.json.JSONObject;
import org.junit.Test;

import java.io.IOException;
import java.io.InputStream;
import java.nio.charset.StandardCharsets;
import java.util.List;

public final class StateSnapshotContractTest {
    private static final List<String> FIXTURES = List.of(
            "healthy.json",
            "zcode_stale.json",
            "commandcode_auth_error.json",
            "backend_degraded.json",
            "task_failed.json"
    );

    @Test
    public void allCanonicalSchemaV1FixturesParseWithProductionParser() throws Exception {
        for (String name : FIXTURES) {
            String json = fixture(name);
            JSONObject root = new JSONObject(json);
            StateSnapshot state = StateSnapshot.parse(json);

            assertEquals("fixture=" + name, 1, root.getInt("schemaVersion"));
            assertEquals("fixture=" + name,
                    root.getJSONObject("overall").getString("status"),
                    state.overallStatus);

            JSONObject zcode = root.getJSONObject("zcode");
            assertEquals("fixture=" + name,
                    zcode.getJSONObject("health").getString("status"),
                    state.zcodeHealth);
            JSONArray zcodeTasks = zcode.optJSONArray("tasks");
            if (zcodeTasks == null) {
                assertNull("fixture=" + name, state.tasks);
            } else {
                assertNotNull("fixture=" + name, state.tasks);
                assertEquals("fixture=" + name, zcodeTasks.length(), state.tasks.size());
            }

            JSONObject commandCode = root.getJSONObject("commandCode");
            assertEquals("fixture=" + name,
                    commandCode.getJSONObject("health").getString("status"),
                    state.commandHealth);
            if (commandCode.optJSONObject("usage") == null) {
                assertNull("fixture=" + name, state.plan);
                assertNull("fixture=" + name, state.creditRemaining);
                assertNull("fixture=" + name, state.fiveHour);
                assertNull("fixture=" + name, state.weekly);
            }
        }
    }

    @Test
    public void healthyFixturePreservesDashboardFields() throws Exception {
        StateSnapshot state = StateSnapshot.parse(fixture("healthy.json"));

        assertEquals("live", state.overallStatus);
        assertEquals("ok", state.zcodeHealth);
        assertEquals(Integer.valueOf(1), state.running);
        assertEquals(Integer.valueOf(1), state.waiting);
        assertEquals(Integer.valueOf(0), state.failed);
        assertEquals(Integer.valueOf(3), state.completed);
        assertNotNull(state.tasks);
        assertEquals(2, state.tasks.size());

        StateSnapshot.TaskItem running = state.tasks.get(0);
        assertEquals("task-running-1", running.id);
        assertEquals("running", running.status);
        assertEquals("Refactor agent pipeline", running.title);
        assertEquals("backend", running.workspace);
        assertEquals("pytest tests/api", running.activity);
        assertEquals(Integer.valueOf(1198), running.durationSeconds);
        assertEquals(Integer.valueOf(428), running.additions);
        assertEquals(Integer.valueOf(103), running.deletions);

        assertEquals("ok", state.commandHealth);
        assertEquals("example-plan", state.plan);
        assertEquals(53.72, state.creditRemaining, 0.0001);
        assertEquals(70.0, state.creditLimit, 0.0001);
        assertEquals("USD", state.creditUnit);
        assertNotNull(state.fiveHour);
        assertEquals(81.0, state.fiveHour.usedPercent, 0.0001);
        assertNotNull(state.weekly);
        assertEquals(63.0, state.weekly.usedPercent, 0.0001);
    }

    @Test
    public void unknownOptionalFieldsRemainForwardCompatible() throws Exception {
        JSONObject root = new JSONObject(fixture("healthy.json"));
        root.put("futureTopLevelField", new JSONObject().put("ignored", true));
        root.getJSONObject("zcode").put("futureZCodeField", 42);
        root.getJSONObject("commandCode").put("futureCommandField", "ignored");

        StateSnapshot state = StateSnapshot.parse(root.toString());
        assertEquals("live", state.overallStatus);
        assertTrue(state.tasks != null && !state.tasks.isEmpty());
    }

    @Test(expected = StateSnapshot.IncompatibleSchemaException.class)
    public void futureSchemaVersionIsRejectedExplicitly() throws Exception {
        JSONObject root = new JSONObject(fixture("healthy.json"));
        root.put("schemaVersion", 2);
        StateSnapshot.parse(root.toString());
    }

    private String fixture(String name) throws IOException {
        ClassLoader loader = StateSnapshotContractTest.class.getClassLoader();
        try (InputStream input = loader.getResourceAsStream(name)) {
            assertNotNull("missing shared contract fixture: " + name, input);
            return new String(input.readAllBytes(), StandardCharsets.UTF_8);
        }
    }
}
