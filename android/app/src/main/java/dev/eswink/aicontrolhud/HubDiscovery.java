package dev.eswink.aicontrolhud;

import org.json.JSONObject;

import java.io.IOException;
import java.net.DatagramPacket;
import java.net.DatagramSocket;
import java.net.Inet4Address;
import java.net.InetAddress;
import java.net.InterfaceAddress;
import java.net.NetworkInterface;
import java.net.SocketTimeoutException;
import java.nio.charset.StandardCharsets;
import java.util.Enumeration;
import java.util.LinkedHashSet;
import java.util.Set;

final class HubDiscovery {
    static final int DISCOVERY_PORT = 8788;
    static final int DISCOVERY_SCHEMA = 1;
    static final String DISCOVERY_SERVICE = "ai-control-hud";
    private static final byte[] REQUEST = "AI_CONTROL_HUD_DISCOVER_V1".getBytes(StandardCharsets.US_ASCII);

    Result discover(int timeoutMs) throws IOException {
        if (timeoutMs < 100 || timeoutMs > 10000) {
            throw new IllegalArgumentException("discovery timeout must be within 100..10000 ms");
        }
        long deadline = System.currentTimeMillis() + timeoutMs;
        try (DatagramSocket socket = new DatagramSocket()) {
            socket.setBroadcast(true);
            int sent = 0;
            for (InetAddress address : broadcastAddresses()) {
                try {
                    socket.send(new DatagramPacket(REQUEST, REQUEST.length, address, DISCOVERY_PORT));
                    sent++;
                } catch (IOException ignored) {
                    // Continue across interfaces; one blocked broadcast route must not abort discovery.
                }
            }
            if (sent == 0) throw new IOException("unable to send hub discovery broadcast");

            byte[] buffer = new byte[2048];
            while (System.currentTimeMillis() < deadline) {
                int remaining = (int) Math.max(1L, deadline - System.currentTimeMillis());
                socket.setSoTimeout(Math.min(250, remaining));
                DatagramPacket packet = new DatagramPacket(buffer, buffer.length);
                try {
                    socket.receive(packet);
                } catch (SocketTimeoutException timeout) {
                    continue;
                }
                Result result = parse(packet);
                if (result != null) return result;
            }
        }
        throw new IOException("no AI Control Hub discovered on local network");
    }

    private static Result parse(DatagramPacket packet) {
        try {
            String json = new String(
                    packet.getData(),
                    packet.getOffset(),
                    packet.getLength(),
                    StandardCharsets.UTF_8
            );
            JSONObject body = new JSONObject(json);
            if (!DISCOVERY_SERVICE.equals(body.optString("service"))) return null;
            if (body.optInt("schemaVersion", -1) != DISCOVERY_SCHEMA) return null;
            String hubId = body.optString("hubId", "").trim();
            String scheme = body.optString("scheme", "").trim().toLowerCase();
            int httpPort = body.optInt("httpPort", -1);
            if (hubId.isEmpty()) return null;
            if (!"http".equals(scheme) && !"https".equals(scheme)) return null;
            if (httpPort < 1 || httpPort > 65535) return null;
            String source = packet.getAddress().getHostAddress();
            if (source == null || source.isEmpty()) return null;
            return new Result(
                    buildBaseUrl(scheme, source, httpPort),
                    hubId,
                    body.optString("hubVersion", "")
            );
        } catch (Exception ignored) {
            return null;
        }
    }

    static String buildBaseUrl(String scheme, String host, int port) {
        return scheme + "://" + host + ":" + port;
    }

    private static Set<InetAddress> broadcastAddresses() throws IOException {
        LinkedHashSet<InetAddress> result = new LinkedHashSet<>();
        result.add(InetAddress.getByName("255.255.255.255"));
        Enumeration<NetworkInterface> interfaces = NetworkInterface.getNetworkInterfaces();
        if (interfaces == null) return result;
        while (interfaces.hasMoreElements()) {
            NetworkInterface network = interfaces.nextElement();
            try {
                if (!network.isUp() || network.isLoopback()) continue;
            } catch (Exception ignored) {
                continue;
            }
            for (InterfaceAddress address : network.getInterfaceAddresses()) {
                InetAddress local = address.getAddress();
                InetAddress broadcast = address.getBroadcast();
                if (local instanceof Inet4Address && broadcast instanceof Inet4Address) {
                    result.add(broadcast);
                }
            }
        }
        return result;
    }

    static final class Result {
        final String baseUrl;
        final String hubId;
        final String hubVersion;

        Result(String baseUrl, String hubId, String hubVersion) {
            this.baseUrl = baseUrl;
            this.hubId = hubId;
            this.hubVersion = hubVersion;
        }
    }
}
