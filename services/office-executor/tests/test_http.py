import http.client
from http.server import ThreadingHTTPServer
import json
from pathlib import Path
import tempfile
import threading
import unittest

from office_executor.__main__ import make_handler
from office_executor.supervisor import Settings
from test_executor import FakeSupervisor, request


class HTTPTest(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.supervisor = FakeSupervisor(Settings(image="sha256:" + "a" * 64, state_dir=Path(self.temp.name)))
        self.token = "only-a-test-token-with-at-least-32-characters"
        self.server = ThreadingHTTPServer(("127.0.0.1", 0), make_handler(self.supervisor, self.token))
        self.thread = threading.Thread(target=self.server.serve_forever, daemon=True)
        self.thread.start()

    def tearDown(self):
        self.server.shutdown()
        self.server.server_close()
        self.thread.join()
        self.temp.cleanup()

    def call(self, body, headers=None, path="/v1/runs/run_123"):
        connection = http.client.HTTPConnection("127.0.0.1", self.server.server_port, timeout=3)
        base = {"Content-Type": "application/json", "Authorization": "Bearer " + self.token,
                "X-Office-Operation-ID": "op_456"}
        base.update(headers or {})
        connection.request("POST", path, body, base)
        response = connection.getresponse()
        result = response.status, json.loads(response.read())
        connection.close()
        return result

    def test_private_authorization_and_bound_operation_identity(self):
        body = json.dumps(request())
        self.assertEqual(self.call(body, {"Authorization": "Bearer incorrect"})[0], 401)
        self.assertEqual(self.call(body, {"Authorization": "Bearer \xff"})[0], 401)
        self.assertEqual(self.call(body, {"X-Office-Operation-ID": "incorrect"})[0], 400)
        self.assertEqual(self.call(body, path="/v1/runs/another")[0], 400)
        self.assertEqual(self.supervisor.calls, 0)
        code, data = self.call(body)
        self.assertEqual(code, 200)
        self.assertEqual(data["status"], "completed")
        self.assertEqual(self.supervisor.calls, 1)

    def test_rejects_ambiguous_json_and_chunked_requests(self):
        self.assertEqual(self.call('{"version":1,"version":2}')[0], 400)
        self.assertEqual(self.call("{}", {"Transfer-Encoding": "chunked"})[0], 400)
        self.assertEqual(self.supervisor.calls, 0)


if __name__ == "__main__":
    unittest.main()
