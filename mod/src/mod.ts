import { IPreSptLoadModAsync } from "@spt/models/external/IPreSptLoadModAsync";
import { ILogger } from "@spt/models/spt/utils/ILogger";
import { readFile, writeFile } from "fs/promises";
import path from "path";
import { DependencyContainer } from "tsyringe";
import config from "../config/config.json";
import { applyOperation, Operation } from "../vendored/fast-json-patch";

type Config = Record<string, Operation[]>;

class Mod implements IPreSptLoadModAsync {
  public async preSptLoadAsync(container: DependencyContainer) {
    const logger = container.resolve<ILogger>("WinstonLogger");

    let sptFolder: string;
    if (process.env["SPT_FOLDER"]) {
      sptFolder = process.env["SPT_FOLDER"];
    } else {
      sptFolder = path.resolve(__filename, "..", "..", "..", "..", "..");
    }
    logger.info(`spt folder: ${sptFolder}`);

    logger.info("applying patches");

    for (let [file, patches] of Object.entries(config as Config)) {
      if (file.startsWith("/")) {
        logger.warning(`ignoring non-relative path ${file}`);
        continue;
      }

      const fullPath = path.join(sptFolder, file);
      const data = JSON.parse((await readFile(file)).toString());

      for (const patch of patches) {
        logger.info(`${fullPath}: ${JSON.stringify(patch)}`);
        applyOperation(data, patch);
      }

      await writeFile(file, JSON.stringify(data, null, 2));
    }
  }
}

export const mod = new Mod();
