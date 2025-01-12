import { IPreSptLoadModAsync } from "@spt/models/external/IPreSptLoadModAsync";
import { ILogger } from "@spt/models/spt/utils/ILogger";
import { readFile, writeFile } from "fs/promises";
import path from "path";
import { DependencyContainer } from "tsyringe";
import patches from "../config/config.json";
import { applyOperation, Operation } from "../vendored/fast-json-patch";

/**
 * The config payload (- a mapping of relative file paths to list of file patches)
 */
type ConfigPatches = Record<string, Operation[]>;

/**
 * Options used to construct a Patcher instance
 */
interface PatcherOpts {
  logger: ILogger;
  sptFolder: string;
}

const sectionRegex = new RegExp(/^\[([^\]]+)\]/);

/**
 * Applies file patches to a variety of different file types.
 */
class Patcher {
  logger: ILogger;
  sptFolder: string;

  constructor({ logger, sptFolder }: PatcherOpts) {
    this.logger = logger;
    this.sptFolder = sptFolder;
  }

  /**
   * Calculates the total number of patches in a ConfigPatches object
   *
   * @param patches the ConfigPatches object
   * @returns the number of patches in the ConfigPatches object
   */
  getNumPatches(patches: ConfigPatches) {
    let numPatches = 0;
    for (const patchList of Object.values(patches)) {
      numPatches += patchList.length;
    }
    return numPatches;
  }

  /**
   * Normalizes a path.
   * Ensures the provided relPath is relative and joins it to the stored sptFolder path.
   *
   * @param relPath a relative path
   * @returns the full path from an spt folder
   */
  normalizePath(relPath: string) {
    if (relPath.startsWith("/")) {
      throw new Error(`path ${path} is not absolute`);
    }
    return path.join(this.sptFolder, relPath);
  }

  /**
   * Applies a patch to a cfg file.
   *
   * @param path the path to a cfg file
   * @param patch the patch to apply
   */
  async applyCfgPatch(path: string, patch: Operation) {
    // validate patch
    if (patch.op !== "replace") {
      throw new Error(`patch op ${patch.op} must be 'replace'`);
    }
    const pathParts = patch.path.split("/");
    if (pathParts.length !== 3) {
      throw new Error(`patch path ${patch.path} must be /section/key`);
    }
    const [section, key] = pathParts.slice(1);
    const value = `${patch.value}`;

    // search file for section + key, perform replacement
    const lines = (await readFile(path)).toString().split("\n");
    let currentSection: string | null = null;
    for (let index = 0; index < lines.length; index++) {
      let line = lines[index];
      const trimmedLine = line.trim();

      const sectionMatch = trimmedLine.match(sectionRegex);
      if (sectionMatch) {
        // new section found - set as current section
        currentSection = sectionMatch[1];
        continue;
      }

      const parts = trimmedLine.split("=", 1);
      const currentKey = parts[0].trim();
      if (currentSection === section && currentKey == key) {
        // current line is a key within the target section - perform replacement
        lines[index] = `${key} = ${value}`;
        break;
      }
    }

    // write updated cfg file
    await writeFile(path, lines.join("\n"));
  }

  /**
   * Applies a patch to a JSON file
   *
   * @param path the path to the json file
   * @param patch the patch to apply
   */
  async applyJsonPatch(path: string, patch: Operation) {
    const data = JSON.parse((await readFile(path)).toString());
    applyOperation(data, patch);
    await writeFile(path, JSON.stringify(data, null, 2));
  }

  /**
   * Applies a patch to a file
   *
   * @param path the path to the file
   * @param patch the patch to apply
   */
  async applyPatch(path: string, patch: Operation) {
    const normalizedPath = this.normalizePath(path);
    this.logger.info(`apply patch: ${path} (${JSON.stringify(patch)})`);
    if (normalizedPath.endsWith(".cfg")) {
      await this.applyCfgPatch(normalizedPath, patch);
    } else if (normalizedPath.endsWith(".json")) {
      await this.applyJsonPatch(normalizedPath, patch);
    } else {
      throw new Error(`unsupported file ${normalizedPath}`);
    }
  }

  /**
   * Applies a collection of config patches.
   *
   * @param patches the patches to apply
   */
  async applyPatches(patches: ConfigPatches) {
    this.logger.info(`applying ${this.getNumPatches(patches)} patches`);

    for (const [path, patchList] of Object.entries(patches)) {
      for (const patch of patchList) {
        await this.applyPatch(path, patch);
      }
    }
  }
}

/**
 * Implements the SPT extension point that ultimately loads this mod's functionality
 */
class Mod implements IPreSptLoadModAsync {
  patcher: Patcher;

  /**
   * Gets the root SPT folder.
   *
   * This can normally be inferred by the script's location - the SPT_FOLDER environment
   * variable allows the location to be hardcoded during development, where the mod.ts file
   * is often symlinked into an SPT installation from another location on the computer.
   *
   * @returns a string path
   */
  getSptFolder() {
    if (process.env["SPT_FOLDER"]) {
      return process.env["SPT_FOLDER"];
    } else {
      return path.resolve(__filename, "..", "..", "..", "..", "..");
    }
  }

  /**
   * Implements IPreSptLoadModAsync.
   *
   * Grabs necessary data, creates a patcher and applies patches.
   *
   * @param container the DI container provided by SPT
   */
  public async preSptLoadAsync(container: DependencyContainer) {
    const logger = container.resolve<ILogger>("WinstonLogger");
    logger.info("mod loaded");

    this.patcher = new Patcher({
      logger: container.resolve<ILogger>("WinstonLogger"),
      sptFolder: this.getSptFolder(),
    });
    await this.patcher.applyPatches(patches as ConfigPatches);
  }
}

export const mod = new Mod();
