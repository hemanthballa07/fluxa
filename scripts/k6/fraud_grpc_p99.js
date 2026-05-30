import grpc from 'k6/net/grpc';
import { check } from 'k6';

const client = new grpc.Client();
client.load(['../../proto'], 'fraud/v1/fraud_eval.proto');

export const options = {
  scenarios: {
    ramp: {
      executor: 'constant-arrival-rate',
      rate: 500,
      timeUnit: '1s',
      duration: '30s',
      preAllocatedVUs: 50,
      maxVUs: 200,
    },
  },
  thresholds: {
    grpc_req_duration: ['p(99)<50'],
  },
};

export default function () {
  if (__ITER === 0) client.connect('localhost:9095', { plaintext: true });

  const flag = __ITER % 3 === 0;
  const resp = client.invoke('fluxa.fraud.v1.FraudEval/EvaluateTransaction', {
    event_id: `k6-${__VU}-${__ITER}`,
    user_id: `u-${__VU}`,
    amount: flag ? 15000 : 100,
    currency: 'USD',
    merchant: 'acme',
    transaction_time: new Date().toISOString(),
  });

  check(resp, { 'status OK': (r) => r && r.status === grpc.StatusOK });
}
