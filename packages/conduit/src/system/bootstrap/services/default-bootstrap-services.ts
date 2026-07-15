import type { ApplicationBootstrapService } from "../interfaces/application-bootstrap.js";
import { ConfigurationBootstrapService } from "./configuration-bootstrap-service.js";
import { CoreBootstrapService } from "./core-bootstrap-service.js";
import { FeaturesBootstrapService } from "./features-bootstrap-service.js";
import { RefinementBootstrapService } from "./refinement-bootstrap-service.js";
import { RolesBootstrapService } from "./roles-bootstrap-service.js";
import { RunsBootstrapService } from "./runs-bootstrap-service.js";
import { UpdatesBootstrapService } from "../../../domains/updates/services/updates-bootstrap-service.js";

export function createDefaultBootstrapServices(
  updatesService: ApplicationBootstrapService = new UpdatesBootstrapService(),
): readonly ApplicationBootstrapService[] {
  return [
    new CoreBootstrapService(),
    new ConfigurationBootstrapService(),
    new FeaturesBootstrapService(),
    new RolesBootstrapService(),
    new RefinementBootstrapService(),
    new RunsBootstrapService(),
    updatesService,
  ];
}
