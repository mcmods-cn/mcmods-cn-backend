export function classifyLoadResponse(request, status, code = "") {
  const expectedStatuses = request.expectedStatuses;
  const statusExpected = expectedStatuses
    ? expectedStatuses.includes(status)
    : status >= 200 && status < 400;
  if (!statusExpected) return { expected: false, expectedReject: false };

  const expectedReject = status >= 400 && status < 500;
  if (expectedReject && request.expectedRiskCodes?.length && !request.expectedRiskCodes.includes(code)) {
    return { expected: false, expectedReject: false };
  }
  return { expected: true, expectedReject };
}

export function loadFailureRate(failures, completed) {
  if (!completed) return 0;
  return Math.round((failures / completed) * 1_000_000) / 1_000_000;
}

export function summarizeLoadOutcomes({
  outcomes,
  networkErrors,
  completed,
  minimumSuccessfulResponses,
  minimumRiskRejects,
  requiredCodes,
}) {
  const expectedResponses = outcomes.filter((outcome) => outcome.expected).length;
  const expectedRejects = outcomes.filter((outcome) => outcome.expectedReject).length;
  const successfulResponses = outcomes.filter((outcome) => outcome.expected && !outcome.expectedReject).length;
  const unexpectedResponses = outcomes.length - expectedResponses;
  const observedCodes = new Set(outcomes.map((outcome) => outcome.code).filter(Boolean));
  const missingRequiredCodes = requiredCodes.filter((code) => !observedCodes.has(code));
  const failureReasons = [];
  if (unexpectedResponses) failureReasons.push(`${unexpectedResponses} unexpected HTTP responses`);
  if (networkErrors) failureReasons.push(`${networkErrors} network errors`);
  if (successfulResponses < minimumSuccessfulResponses) {
    failureReasons.push(`successful response volume ${successfulResponses} is below ${minimumSuccessfulResponses}`);
  }
  if (expectedRejects < minimumRiskRejects) {
    failureReasons.push(`expected risk rejection volume ${expectedRejects} is below ${minimumRiskRejects}`);
  }
  if (missingRequiredCodes.length) {
    failureReasons.push(`missing required response codes: ${missingRequiredCodes.join(", ")}`);
  }
  return {
    expectedResponses,
    expectedRejects,
    successfulResponses,
    unexpectedResponses,
    failureRate: loadFailureRate(unexpectedResponses + networkErrors, completed),
    missingRequiredCodes,
    failureReasons,
  };
}
