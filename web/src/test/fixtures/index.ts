/**
 * Single entry point for contract fixtures. A later backend step generates these JSON files from the
 * Go structs into this same directory; tests must only import fixtures from here.
 */
import task from "./task.json";
import approval from "./approval.json";
import artifact from "./artifact.json";
import presign from "./presign.json";
import submitResult from "./submit-result.json";
import errorEnvelope from "./error-envelope.json";
import systemStatus from "./system-status.json";

export const fixtures = { task, approval, artifact, presign, submitResult, errorEnvelope, systemStatus };

/** Wraps data the way internal/api/httputil.JSON does. */
export const envelope = (data: unknown) => ({ success: true, data });
