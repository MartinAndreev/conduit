declare const __CONDUIT_STANDALONE__: boolean;

export const conduitStandaloneBuild =
  typeof __CONDUIT_STANDALONE__ !== "undefined" &&
  __CONDUIT_STANDALONE__ === true;
