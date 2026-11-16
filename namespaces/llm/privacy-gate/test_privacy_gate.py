"""Offline tests: python -m unittest -v   (from this directory; stdlib only).

All values are obviously fake (example.com/.invalid, 555 numbers, published
test card / IBAN numbers). Network use is loopback only (fake upstreams).
"""

import base64
import http.client
import json
import os
import tempfile
import threading
import time
import unittest
import urllib.parse
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

import privacy_gate as pg
import rules


def body(*msgs, **kw):
    b = {"model": "claude", "messages": [{"role": r, "content": c} for r, c in msgs]}
    b.update(kw)
    return b


class RuleTests(unittest.TestCase):
    def hits(self, text):
        return rules.scan(text)

    def test_clean_text(self):
        self.assertEqual(self.hits("How do I write a quicksort in Rust? Version 1.2.3, port 8080, 2026-11-15."), [])

    def test_email(self):
        self.assertIn("email", self.hits("mail jane.doe@example.com please"))

    def test_phone(self):
        self.assertIn("phone", self.hits("call (555) 555-0100 tonight"))
        self.assertIn("phone", self.hits("ring +44 20 7946 0000"))
        self.assertNotIn("phone", self.hits("build 20261115123456"))

    def test_card_luhn(self):
        self.assertIn("card", self.hits("card 4111 1111 1111 1111 exp 12/30"))
        self.assertNotIn("card", self.hits("card 4111 1111 1111 1112"))

    def test_iban(self):
        self.assertIn("iban", self.hits("IBAN GB82 WEST 1234 5698 7654 32"))
        self.assertNotIn("iban", self.hits("IBAN GB82 WEST 1234 5698 7654 33"))

    def test_ssn(self):
        self.assertIn("ssn", self.hits("ssn 123-45-6789"))
        self.assertNotIn("ssn", self.hits("id 000-12-3456"))

    def test_address_and_dob(self):
        self.assertIn("address", self.hits("I live at 742 Evergreen Terrace now"))
        self.assertIn("dob", self.hits("DOB: 01/02/1990"))
        self.assertNotIn("dob", self.hits("released 01/02/1990"))

    def test_private_ip(self):
        self.assertIn("private_ip", self.hits("ssh 192.168.1.50"))
        self.assertIn("private_ip", self.hits("pod 10.244.0.5"))
        self.assertNotIn("private_ip", self.hits("dns 1.1.1.1 and 8.8.8.8"))

    def test_secrets(self):
        self.assertIn("private_key", self.hits("-----BEGIN OPENSSH PRIVATE KEY-----\nAAAA"))
        jwt = "eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiJmYWtlLXVzZXIifQ.ZmFrZXNpZ25hdHVyZTEyMw"
        self.assertIn("jwt", self.hits("token " + jwt))
        self.assertIn("cloud_key", self.hits("AKIAABCDEFGHIJKLMNOP"))
        self.assertIn("vcs_token", self.hits("ghp_" + "a" * 36))
        self.assertIn("api_token", self.hits("sk-ant-" + "x1" * 15))
        self.assertIn("bearer", self.hits("Authorization: Bearer abcdefghijklmnopqrstuvwxyz012345"))
        self.assertIn("url_credentials", self.hits("postgres://app:hunter2pw@db.invalid/app"))
        self.assertIn("password_assign", self.hits('password = "Fake-Passw0rd!"'))
        self.assertNotIn("password_assign", self.hits("password: ${DB_PASSWORD}"))
        self.assertNotIn("password_assign", self.hits("api_key: none"))

    def test_marker(self):
        self.assertIn("marker", self.hits("This memo is CONFIDENTIAL, do not share."))

    def test_encoded_variants(self):
        b64 = base64.b64encode(b"contact jane.doe@example.com for details").decode()
        self.assertIn("email", self.hits("blob=" + b64))
        self.assertIn("email", self.hits("q=jane.doe%40example.com"))

    def test_disabled(self):
        self.assertEqual(rules.scan("ssh 192.168.1.50", disabled=["private_ip"]), [])

    def test_validators(self):
        self.assertTrue(rules.luhn_ok("4111111111111111"))
        self.assertTrue(rules.iban_ok("DE89 3704 0044 0532 0130 00"))


class ConversationIdTests(unittest.TestCase):
    HDRS = ["x-conversation-id", "x-openwebui-chat-id", "x-session-id"]

    def test_header_priority(self):
        b = body(("user", "hi"))
        a, src = pg.conversation_id({"X-Session-Id": "s1", "X-OpenWebUI-Chat-Id": "c1"}, b, self.HDRS)
        self.assertEqual(src, "x-openwebui-chat-id")
        a2, src2 = pg.conversation_id({"x-conversation-id": "h1", "x-session-id": "s1"}, b, self.HDRS)
        self.assertEqual(src2, "x-conversation-id")
        self.assertNotEqual(a, a2)
        self.assertEqual(len(a), 64)
        self.assertNotIn("c1", a)

    def test_fingerprint(self):
        b1 = body(("system", "S"), ("user", "first"), ("assistant", "x"), ("user", "second"))
        b2 = body(("system", "S"), ("user", "first"))
        b3 = body(("system", "S"), ("user", "other"))
        f1, src = pg.conversation_id({}, b1, self.HDRS)
        self.assertEqual(src, "fingerprint")
        self.assertEqual(f1, pg.conversation_id({}, b2, self.HDRS)[0])
        self.assertNotEqual(f1, pg.conversation_id({}, b3, self.HDRS)[0])

    def test_blank_header_ignored(self):
        _, src = pg.conversation_id({"x-conversation-id": "  "}, body(("user", "hi")), self.HDRS)
        self.assertEqual(src, "fingerprint")


class StoreTests(unittest.TestCase):
    def test_persistence_and_ttl(self):
        with tempfile.TemporaryDirectory() as d:
            path = os.path.join(d, "t.sqlite3")
            s = pg.TaintStore(path, ttl_days=1)
            self.assertTrue(s.taint("abc", ["email"]))
            self.assertFalse(s.taint("abc", ["email"]))
            s2 = pg.TaintStore(path, ttl_days=1)  # reopen: survives restarts
            self.assertTrue(s2.is_tainted("abc"))
            self.assertFalse(s2.is_tainted("abc", now=time.time() + 2 * 86400))
            s2.purge(now=time.time() + 2 * 86400)
            self.assertEqual(s2.count(), 0)

    def test_clean_prefix_cleared_on_taint(self):
        s = pg.TaintStore(":memory:", 30)
        s.set_clean_prefix("c", 3, "d")
        self.assertEqual(s.clean_prefix("c"), (3, "d"))
        s.taint("c", ["classifier"])
        self.assertEqual(s.clean_prefix("c"), (0, ""))


def make_gate(chunk_fn, **cfg_kw):
    cfg = pg.Config()
    for k, v in cfg_kw.items():
        setattr(cfg, k, v)
    return pg.Gate(cfg, pg.TaintStore(":memory:", 30), chunk_fn=chunk_fn)


class DecisionTests(unittest.TestCase):
    def test_clean(self):
        calls = []
        g = make_gate(lambda cfg, text: (calls.append(text), (False, []))[1])
        d = g.decide({}, body(("user", "explain the borrow checker")))
        self.assertFalse(d.sensitive)
        self.assertEqual(len(calls), 1)

    def test_rule_hit_taints_and_skips_classifier(self):
        calls = []
        g = make_gate(lambda cfg, text: (calls.append(1), (False, []))[1])
        h = {"x-conversation-id": "conv-1"}
        d = g.decide(h, body(("user", "my email is jane.doe@example.com")))
        self.assertTrue(d.sensitive)
        self.assertEqual(d.reasons, ["email"])
        self.assertEqual(calls, [])
        d2 = g.decide(h, body(("user", "now something harmless")))
        self.assertEqual(d2.reasons, ["tainted"])

    def test_tool_results_are_scanned(self):
        g = make_gate(lambda cfg, text: (False, []))
        b = {"messages": [
            {"role": "user", "content": "check it"},
            {"role": "assistant", "content": None, "tool_calls": [
                {"id": "1", "type": "function", "function": {"name": "sh", "arguments": "{\"cmd\": \"cat x\"}"}}]},
            {"role": "tool", "tool_call_id": "1", "content": "token=ghp_" + "b" * 36}]}
        d = g.decide({}, b)
        self.assertIn("vcs_token", d.reasons)

    def test_media_is_sensitive(self):
        g = make_gate(lambda cfg, text: (False, []))
        b = {"messages": [{"role": "user", "content": [
            {"type": "text", "text": "what is this"},
            {"type": "image_url", "image_url": {"url": "data:image/png;base64,AAAA"}}]}]}
        self.assertEqual(g.decide({}, b).reasons, ["media"])

    def test_classifier_sensitive(self):
        g = make_gate(lambda cfg, text: (True, ["health"]))
        d = g.decide({}, body(("user", "my doctor changed my medication")))
        self.assertTrue(d.sensitive)
        self.assertEqual(d.reasons, ["classifier"])
        self.assertEqual(d.categories, ["health"])

    def test_fail_closed_on_error(self):
        def boom(cfg, text):
            raise pg.ClassifierError("clf_timeout")
        d = make_gate(boom).decide({}, body(("user", "harmless")))
        self.assertTrue(d.sensitive)
        self.assertEqual(d.reasons, ["clf_timeout"])

    def test_fail_closed_on_unexpected_exception(self):
        def boom(cfg, text):
            raise RuntimeError("x")
        self.assertEqual(make_gate(boom).decide({}, body(("user", "harmless"))).reasons, ["clf_error"])

    def test_fail_closed_on_slow_classifier(self):
        def slow(cfg, text):
            time.sleep(0.5)
            return False, []
        d = make_gate(slow, classifier_budget=0.1).decide({}, body(("user", "harmless")))
        self.assertEqual(d.reasons, ["clf_budget"])

    def test_fail_closed_on_overflow(self):
        g = make_gate(lambda cfg, text: (False, []), classifier_chunk_tokens=10, classifier_max_chunks=2)
        d = g.decide({}, body(("user", "word " * 200)))
        self.assertEqual(d.reasons, ["clf_overflow"])

    def test_classifier_disabled_fails_closed(self):
        g = make_gate(None, classifier_enabled=False)
        self.assertEqual(g.decide({}, body(("user", "harmless"))).reasons, ["clf_disabled"])

    def test_chunks_or_together(self):
        seen = []

        def fn(cfg, text):
            seen.append(text)
            return ("MARK" in text), (["finances"] if "MARK" in text else [])
        g = make_gate(fn, classifier_chunk_tokens=20, classifier_max_chunks=10, chars_per_token=3)
        b = body(("user", "a" * 50), ("assistant", "b" * 50), ("user", "MARK c"))
        d = g.decide({}, b)
        self.assertGreater(len(seen), 1)
        self.assertTrue(all(len(t) <= 60 for t in seen))
        self.assertEqual(d.reasons, ["classifier"])

    def test_incremental_classification(self):
        seen = []
        g = make_gate(lambda cfg, text: (seen.append(text), (False, []))[1])
        h = {"x-conversation-id": "inc"}
        g.decide(h, body(("system", "sys"), ("user", "first question")))
        d = g.decide(h, body(("system", "sys"), ("user", "first question"), ("assistant", "answer"),
                             ("user", "second question")))
        self.assertEqual(d.clf_skipped_messages, 2)
        self.assertNotIn("first question", seen[-1])
        self.assertIn("second question", seen[-1])
        # edited history -> full re-classification
        d = g.decide(h, body(("system", "sys"), ("user", "EDITED"), ("user", "third")))
        self.assertEqual(d.clf_skipped_messages, 0)

    def test_parse_verdict(self):
        mk = lambda c: json.dumps({"choices": [{"message": {"content": c}}]}).encode()
        self.assertEqual(pg.parse_verdict(mk('{"sensitive": false, "categories": [], "reason": ""}')), (False, []))
        self.assertEqual(pg.parse_verdict(mk('{"sensitive": false, "categories": ["legal"], "reason": ""}')),
                         (True, ["legal"]))
        for bad in ("not json", '{"categories": []}', '{"sensitive": "no", "categories": []}'):
            with self.assertRaises(pg.ClassifierError):
                pg.parse_verdict(mk(bad))


class ConfigTests(unittest.TestCase):
    def test_enforce_needs_key(self):
        c = pg.Config()
        c.mode = "enforce"
        with self.assertRaises(SystemExit):
            c.validate()
        c.anthropic_api_key = "fake"
        c.validate()

    def test_bad_mode(self):
        c = pg.Config()
        c.mode = "yolo"
        with self.assertRaises(SystemExit):
            c.validate()


# ------------------------------------------------------------------ HTTP / SSE

SSE_EVENTS = [
    b'data: {"choices":[{"delta":{"content":"Hel"}}]}\n\n',
    b'data: {"choices":[{"delta":{"content":"lo"}}],"timings":{"x":1}}\n\n',
    b": keep-alive comment\n\n",
    b"data: [DONE]\n\n",
]


class FakeUpstream(BaseHTTPRequestHandler):
    protocol_version = "HTTP/1.1"
    received = []

    def log_message(self, *a):
        pass

    def do_POST(self):
        n = int(self.headers.get("Content-Length") or 0)
        req = json.loads(self.rfile.read(n))
        FakeUpstream.received.append((req, dict(self.headers.items())))
        if req.get("stream"):
            self.send_response(200)
            self.send_header("Content-Type", "text/event-stream")
            self.send_header("Transfer-Encoding", "chunked")
            self.end_headers()
            for ev in SSE_EVENTS:
                # split each event across two chunks to prove framing is untouched
                for part in (ev[:7], ev[7:]):
                    self.wfile.write(b"%x\r\n%s\r\n" % (len(part), part))
                    self.wfile.flush()
                    time.sleep(0.01)
            self.wfile.write(b"0\r\n\r\n")
        else:
            data = json.dumps({"id": "x", "model": req["model"], "choices": [
                {"message": {"role": "assistant", "content": "ok"}}]}).encode()
            self.send_response(200)
            self.send_header("Content-Type", "application/json")
            self.send_header("Content-Length", str(len(data)))
            self.end_headers()
            self.wfile.write(data)


def serve(handler):
    srv = ThreadingHTTPServer(("127.0.0.1", 0), handler)
    srv.daemon_threads = True
    threading.Thread(target=srv.serve_forever, daemon=True).start()
    return srv


class HTTPTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.up = serve(FakeUpstream)
        cfg = pg.Config()
        cfg.local_url = f"http://127.0.0.1:{cls.up.server_address[1]}"
        cls.gate = pg.Gate(cfg, pg.TaintStore(":memory:", 30), chunk_fn=lambda c, t: (False, []))
        cls.gw = serve(pg.make_handler(cls.gate))
        cls.port = cls.gw.server_address[1]

    @classmethod
    def tearDownClass(cls):
        cls.gw.shutdown()
        cls.up.shutdown()

    def post(self, payload, headers=None, chunked=False):
        conn = http.client.HTTPConnection("127.0.0.1", self.port, timeout=10)
        data = json.dumps(payload).encode()
        h = {"Content-Type": "application/json", "X-OpenWebUI-User-Email": "nobody@example.invalid"}
        h.update(headers or {})
        if chunked:
            conn.putrequest("POST", "/v1/chat/completions")
            for k, v in h.items():
                conn.putheader(k, v)
            conn.putheader("Transfer-Encoding", "chunked")
            conn.endheaders()
            for i in range(0, len(data), 10):
                part = data[i:i + 10]
                conn.send(b"%x\r\n%s\r\n" % (len(part), part))
            conn.send(b"0\r\n\r\n")
        else:
            conn.request("POST", "/v1/chat/completions", body=data, headers=h)
        return conn, conn.getresponse()

    def test_non_streaming_shadow(self):
        conn, r = self.post(body(("user", "explain monads")))
        data = json.loads(r.read())
        self.assertEqual(r.status, 200)
        self.assertEqual(r.getheader("x-privacy-gate"), "shadow-local")
        self.assertEqual(data["model"], "qwen3.8-27b")
        req, hdrs = FakeUpstream.received[-1]
        self.assertEqual(req["model"], "qwen3.8-27b")
        self.assertNotIn("x-openwebui-user-email", {k.lower() for k in hdrs})
        conn.close()

    def test_sensitive_header(self):
        conn, r = self.post(body(("user", "ssn 123-45-6789")), {"x-conversation-id": "http-1"})
        r.read()
        self.assertEqual(r.getheader("x-privacy-gate"), "local; reason=ssn")
        conn.close()

    def test_chunked_request_body(self):
        conn, r = self.post(body(("user", "chunked hello")), chunked=True)
        self.assertEqual(r.status, 200)
        self.assertEqual(json.loads(r.read())["model"], "qwen3.8-27b")
        conn.close()

    def test_sse_passthrough_framing(self):
        conn, r = self.post(body(("user", "stream please"), stream=True))
        self.assertEqual(r.status, 200)
        self.assertEqual(r.getheader("Content-Type"), "text/event-stream")
        self.assertEqual(r.getheader("x-privacy-gate"), "shadow-local")
        got = r.read()
        self.assertEqual(got, b"".join(SSE_EVENTS))
        conn.close()

    def test_models_health_metrics(self):
        for path, check in (("/v1/models", b'"claude"'), ("/healthz", b'"ok"'),
                            ("/metrics", b"privacy_gate_requests_total")):
            conn = http.client.HTTPConnection("127.0.0.1", self.port, timeout=5)
            conn.request("GET", path)
            r = conn.getresponse()
            self.assertEqual(r.status, 200, path)
            self.assertIn(check, r.read())
            conn.close()

    def test_bad_json(self):
        conn = http.client.HTTPConnection("127.0.0.1", self.port, timeout=5)
        conn.request("POST", "/v1/chat/completions", body=b"{nope", headers={"Content-Type": "application/json"})
        self.assertEqual(conn.getresponse().status, 400)
        conn.close()

    def test_upstream_down_is_502(self):
        cfg = pg.Config()
        cfg.local_url = "http://127.0.0.1:9"  # discard port, nothing listens
        g = pg.Gate(cfg, pg.TaintStore(":memory:", 30), chunk_fn=lambda c, t: (False, []))
        srv = serve(pg.make_handler(g))
        try:
            conn = http.client.HTTPConnection("127.0.0.1", srv.server_address[1], timeout=5)
            conn.request("POST", "/v1/chat/completions", body=json.dumps(body(("user", "x"))).encode(),
                         headers={"Content-Type": "application/json"})
            self.assertEqual(conn.getresponse().status, 502)
        finally:
            srv.shutdown()


if __name__ == "__main__":
    unittest.main()
