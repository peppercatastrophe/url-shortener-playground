import http from 'k6/http';
import { check, sleep } from 'k6';

// Read the short code from an environment variable so the test can
// hit a URL that already exists in the database.
const CODE = __ENV.CODE;
if (!CODE) {
  throw new Error('CODE env var is required: the short code to GET');
}

const BASE = __ENV.BASE_URL || 'http://localhost:3000';

export const options = {
  // Ramp up to the target RPS, hold, then ramp down.
  stages: [
    { duration: '15s', target: 200 },   // warm up
    { duration: '30s', target: 500 },   // hold at 500 VUs
    { duration: '15s', target: 1000 },  // peak
    { duration: '30s', target: 1000 },  // hold at peak
    { duration: '15s', target: 0 },     // ramp down
  ],
  thresholds: {
    // Fail the test if more than 1% of requests error out.
    http_req_failed: ['rate<0.01'],
    // Report p90 and p99, but don't hard-fail — we want the numbers.
    http_req_duration: ['p(99)<500'],
  },
};

export default function () {
  const res = http.get(`${BASE}/${CODE}`, {
    redirects: 0,  // don't follow the 301 — we only care about the lookup
  });
  check(res, {
    'status is 301': (r) => r.status === 301,
  });
}
