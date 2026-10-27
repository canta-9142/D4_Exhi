// State polling of docs/spec/web.md 6. At most one GET is in flight. The next
// one is sent POLL_MS after the previous one completes, or RETRY_MS after a
// failure, and none is scheduled while the page is hidden.

import { getState } from "./api.js";

export const POLL_MS = 500;
export const RETRY_MS = 1000;

export class Poller {
  #onSnapshot;
  #onFailure;
  // Each GET is numbered. Responses to GETs numbered below #minSeq are
  // dropped: they were sent before the last refresh().
  #nextSeq = 1;
  #minSeq = 1;
  #inFlight = false;
  // Send the next GET as soon as the one in flight completes.
  #again = false;
  #timer = null;
  #waiters = [];

  // onSnapshot(snapshot) shows a successful GET and onFailure(result) a
  // failed one, where result is a non-success result of api.js. The page
  // keeps the previous snapshot on failure.
  constructor({ onSnapshot, onFailure }) {
    this.#onSnapshot = onSnapshot;
    this.#onFailure = onFailure;
  }

  start() {
    document.addEventListener("visibilitychange", () => {
      if (document.hidden) {
        this.#cancelTimer();
      } else {
        this.#fetchNow();
      }
    });
    if (!document.hidden) {
      this.#fetchNow();
    }
  }

  // refresh resolves once a GET sent from now on has succeeded and
  // onSnapshot has returned. A GET already in flight is not cancelled, since
  // only one may be in flight, but its response is dropped so that it can
  // neither show the state from before an operation nor resolve the promise.
  // A failed GET is retried like any other, so the promise waits for the
  // retry to succeed.
  refresh() {
    this.#minSeq = this.#nextSeq;
    const done = new Promise((resolve) => this.#waiters.push(resolve));
    this.#fetchNow();
    return done;
  }

  #fetchNow() {
    this.#cancelTimer();
    if (this.#inFlight) {
      this.#again = true;
    } else {
      this.#send();
    }
  }

  async #send() {
    this.#timer = null;
    this.#inFlight = true;
    const seq = this.#nextSeq++;
    const result = await getState();
    let delay = RETRY_MS;
    try {
      // A dropped response leaves #again set by refresh().
      if (seq < this.#minSeq) {
        return;
      }
      if (result.kind !== "success") {
        this.#onFailure(result);
        return;
      }
      this.#onSnapshot(result.body);
      delay = POLL_MS;
      for (const resolve of this.#waiters.splice(0)) {
        resolve();
      }
    } finally {
      // Until here, a refresh() from a callback only sets #again, so that
      // #schedule sends the next GET instead of a second one starting now.
      this.#inFlight = false;
      this.#schedule(delay);
    }
  }

  #schedule(delay) {
    if (this.#again) {
      this.#again = false;
      this.#send();
    } else if (!document.hidden) {
      this.#timer = setTimeout(() => this.#send(), delay);
    }
  }

  #cancelTimer() {
    clearTimeout(this.#timer);
    this.#timer = null;
  }
}
