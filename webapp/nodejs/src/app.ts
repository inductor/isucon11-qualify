import { execFile } from "child_process";
import fs from "fs";
import path from "path";
import { promisify } from "util";

import session from "cookie-session";
import express from "express";
import jwt from "jsonwebtoken";
import morgan from "morgan";
import multer, { MulterError } from "multer";
import mysql, { RowDataPacket } from "mysql2/promise";
import axios from "axios";

interface Config extends RowDataPacket {
  name: string;
  url: string;
}

interface IsuResponse {
  id: number;
  jia_isu_uuid: string;
  name: string;
  character: string;
}

interface Isu extends IsuResponse, RowDataPacket {
  image: Buffer;
  jia_user_id: string;
  created_at: Date;
  updated_at: Date;
}

interface GetIsuListResponse {
  id: number;
  jia_isu_uuid: string;
  name: string;
  character: string;
  latest_isu_condition?: GetIsuConditionResponse;
}

interface InitializeReponse {
  language: string;
}

interface GetMeResponse {
  jia_user_id: string;
}

interface GetIsuConditionResponse {
  jia_isu_uuid: string;
  isu_name: string;
  timestamp: number;
  is_sitting: boolean;
  condition: string;
  condition_level: string;
  message: string;
}

interface IsuConditionRow extends RowDataPacket {
  id: number;
  jia_isu_uuid: string;
  timestamp: Date;
  is_sitting: number;
  condition: string;
  message: string;
  created_at: Date;
}

const sessionName = "isucondition";
// const conditionLimit = 20;
const frontendContentsPath = "../public";
const jwtVerificationKeyPath = "../ec256-public.pem";
const defaultIconFilePath = "../NoImage.jpg";
const defaultJIAServiceUrl = "http://localhost:5000";
// mysqlErrNumDuplicateEntry = 1062
const conditionLevelInfo = "info";
const conditionLevelWarning = "warning";
const conditionLevelCritical = "critical";
// const scoreConditionLevelInfo     = 3
// const scoreConditionLevelWarning  = 2
// const scoreConditionLevelCritical = 1

if (!("POST_ISUCONDITION_TARGET_BASE_URL" in process.env)) {
  console.error("missing: POST_ISUCONDITION_TARGET_BASE_URL");
  process.exit(1);
}
const postIsuConditionTargetBaseURL =
  process.env["POST_ISUCONDITION_TARGET_BASE_URL"];
const dbinfo: mysql.PoolOptions = {
  host: process.env["MYSQL_HOST"] ?? "127.0.0.1",
  port: Number.parseInt(process.env["MYSQL_PORT"] ?? "3306"),
  user: process.env["MYSQL_USER"] ?? "isucon",
  database: process.env["MYSQL_DBNAME"] ?? "isucondition",
  password: process.env["MYSQL_PASS"] || "isucon",
  connectionLimit: 10,
  timezone: "+09:00",
};
const pool = mysql.createPool(dbinfo);
const upload = multer();

const app = express();

app.use("/assets", express.static(frontendContentsPath + "/assets"));
app.use(morgan("combined"));
app.use(
  session({
    secret: process.env["SESSION_KEY"] ?? "isucondition",
    name: sessionName,
    maxAge: 60 * 60 * 24 * 1000 * 30,
  })
);
app.set("cert", fs.readFileSync(jwtVerificationKeyPath));

["/", "/condition", "/isu/:jia_isu_uuid", "/register", "/login"].forEach(
  (frontendPath) => {
    app.get(frontendPath, (_req, res) => {
      res.sendFile(path.resolve("../public", "index.html"));
    });
  }
);

async function getUserIdFromSession(
  req: express.Request,
  db: mysql.Connection
): Promise<[string, number, Error?]> {
  if (!req.session) {
    return ["", 500, new Error("failed to get session")];
  }
  const jiaUserId = req.session["jia_user_id"];
  if (!jiaUserId) {
    return ["", 401, new Error("no session")];
  }

  try {
    const [[count]] = await db.query<(RowDataPacket & { cnt: number })[]>(
      "SELECT COUNT(*) AS `cnt` FROM `user` WHERE `jia_user_id` = ?",
      [jiaUserId]
    );
    if (count.cnt === 0) {
      return ["", 401, new Error("not found: user")];
    }
  } catch (err) {
    return ["", 500, new Error(`db error: ${err}`)];
  }
  return [jiaUserId, 0, undefined];
}

async function getJIAServiceUrl(db: mysql.Connection): Promise<string> {
  const [[config]] = await db.query<Config[]>(
    "SELECT * FROM `isu_association_config` WHERE `name` = ?",
    ["jia_service_url"]
  );
  if (!config) {
    return defaultJIAServiceUrl;
  }
  return config.url;
}

// POST /initialize
// サービスを初期化
app.post("/initialize", async (_req, res) => {
  // TODO

  try {
    const { stderr } = await promisify(execFile)("../sql/init.sh");
    console.log(stderr);
  } catch (err) {
    console.error(`exec init.sh error: ${err}`);
    return res.status(500).send();
  }
  const initializeResponse: InitializeReponse = { language: "nodejs" };
  return res.status(200).json(initializeResponse);
});

// POST /api/auth
// サインアップ・サインイン
app.post("/api/auth", async (req, res) => {
  const db = await pool.getConnection();
  const authHeader = req.headers.authorization ?? "";
  const token = authHeader.startsWith("Bearer ")
    ? authHeader.slice(7)
    : authHeader;
  try {
    const decoded = jwt.verify(token, req.app.get("cert")) as jwt.JwtPayload;
    if (!("jia_user_id" in decoded)) {
      return res.status(400).send("invalid JWT payload");
    }
    const jiaUserId = decoded["jia_user_id"];
    if (typeof jiaUserId !== "string") {
      return res.status(400).send("invalid JWT payload");
    }
    await db.query(
      "INSERT IGNORE INTO user (`jia_user_id`) VALUES (?)",
      jiaUserId
    );
    await db.commit();
    req.session = { jia_user_id: jiaUserId };
    return res.status(200).send();
  } catch (err) {
    console.error(`jwt validation error: ${err}`);
    return res.status(403).send("forbidden");
  } finally {
    db.release();
  }
});

// POST /api/signout
// サインアウト
app.post("/api/signout", async (req, res) => {
  const db = await pool.getConnection();
  try {
    const [_, errStatusCode, err] = await getUserIdFromSession(req, db);
    if (err) {
      if (errStatusCode === 401) {
        return res.status(401).type("text").send("you are not signed in");
      }
      console.error(err);
      return res.status(500).send();
    }

    req.session = null;
    return res.status(200).send();
  } finally {
    db.release();
  }
});

// GET /api/user/me
// サインインしている自分自身の情報を取得
app.get("/api/user/me", async (req, res) => {
  const db = await pool.getConnection();
  try {
    const [jiaUserId, errStatusCode, err] = await getUserIdFromSession(req, db);
    if (err) {
      if (errStatusCode === 401) {
        return res.status(401).type("text").send("you are not signed in");
      }
      console.error(err);
      return res.status(500).send();
    }

    const getMeResponse: GetMeResponse = { jia_user_id: jiaUserId };
    return res.status(200).json(getMeResponse);
  } finally {
    db.release();
  }
});

// GET /api/isu
// ISUの一覧を取得
app.get("/api/isu", async (req, res) => {
  const db = await pool.getConnection();
  try {
    const [jiaUserId, errStatusCode, err] = await getUserIdFromSession(req, db);
    if (err) {
      if (errStatusCode === 401) {
        return res.status(401).type("text").send("you are not signed in");
      }
      console.error(err);
      return res.status(500).send();
    }

    await db.beginTransaction();
    const [isuList] = await db.query<Isu[]>(
      "SELECT * FROM `isu` WHERE `jia_user_id` = ? ORDER BY `id` DESC",
      [jiaUserId]
    );
    const responseList: Array<GetIsuListResponse> = [];
    for (const isu of isuList) {
      let foundLastCondition = true;
      const [[lastCondition]] = await db.query<IsuConditionRow[]>(
        "SELECT * FROM `isu_condition` WHERE `jia_isu_uuid` = ? ORDER BY `timestamp` DESC LIMIT 1",
        [isu.jia_isu_uuid]
      );
      if (!lastCondition) {
        foundLastCondition = false;
      }
      let formattedCondition = undefined;
      if (foundLastCondition) {
        const [conditionLevel, err] = calculateConditionLevel(
          lastCondition.condition
        );
        if (err) {
          console.error(`failed to get condition level: ${err}`);
          return res.status(500).send();
        }
        formattedCondition = {
          jia_isu_uuid: lastCondition.jia_isu_uuid,
          isu_name: isu.name,
          timestamp: lastCondition.timestamp.getTime() / 1000,
          is_sitting: !!lastCondition.is_sitting,
          condition: lastCondition.condition,
          condition_level: conditionLevel,
          message: lastCondition.message,
        };
      }
      responseList.push({
        id: isu.id,
        jia_isu_uuid: isu.jia_isu_uuid,
        name: isu.name,
        character: isu.character,
        latest_isu_condition: formattedCondition,
      });
    }
    await db.commit();
    return res.status(200).json(responseList);
  } catch (err) {
    console.error(`db error: ${err}`);
    await db.rollback();
    return res.status(500).send();
  } finally {
    db.release();
  }
});

interface PostIsuBody {
  jia_isu_uuid: string;
  isu_name: string;
}

// POST /api/isu
// ISUを登録
app.post("/api/isu", (req: express.Request<{}, any, PostIsuBody>, res) => {
  upload.single("image")(req, res, async (uploadErr) => {
    const db = await pool.getConnection();
    try {
      const [jiaUserId, errStatusCode, err] = await getUserIdFromSession(
        req,
        db
      );
      if (err) {
        if (errStatusCode === 401) {
          return res.status(401).type("text").send("you are not signed in");
        }
        console.error(err);
        return res.status(500).send();
      }

      const jiaIsuUUID = req.body.jia_isu_uuid;
      const isuName = req.body.isu_name;
      if (uploadErr instanceof MulterError) {
        // TODO
      }

      let image: Buffer;
      if (!req.file) {
        image = fs.readFileSync(defaultIconFilePath);
      } else {
        image = req.file.buffer;
      }

      await db.beginTransaction();
      await db.query(
        "INSERT INTO `isu` (`jia_isu_uuid`, `name`, `image`, `jia_user_id`) VALUES (?, ?, ?, ?)",
        [jiaIsuUUID, isuName, image, jiaUserId]
      );

      // TODO: check duplicate

      const targetUrl = (await getJIAServiceUrl(db)) + "/api/activate";

      let isuFromJIA: { character: string };
      try {
        const response = await axios.post(targetUrl, {
          target_base_url: postIsuConditionTargetBaseURL,
          isu_uuid: jiaIsuUUID,
        });
        if (response.status !== 202) {
          console.error(
            `JIAService returned error: status code ${response.status}, message: ${response.data}`
          );
          return res.status(response.status).send("JIAService returned error");
        }
        isuFromJIA = response.data;
      } catch (err) {
        console.error(`failed to request to JIAService: ${err}`);
        return res.status(500).send();
      }

      await db.query(
        "UPDATE `isu` SET `character` = ? WHERE  `jia_isu_uuid` = ?",
        [isuFromJIA.character, jiaIsuUUID]
      );
      const [[isu]] = await db.query<Isu[]>(
        "SELECT * FROM `isu` WHERE `jia_user_id` = ? AND `jia_isu_uuid` = ?",
        [jiaUserId, jiaIsuUUID]
      );
      if (!isu) {
        throw new Error();
      }
      await db.commit();

      const isuResponse: IsuResponse = {
        id: isu.id,
        jia_isu_uuid: isu.jia_isu_uuid,
        name: isu.name,
        character: isu.character,
      };
      return res.status(201).send(isuResponse);
    } catch (err) {
      console.error(`db error: ${err}`);
      await db.rollback();
      return res.status(500).send();
    } finally {
      db.release();
    }
  });
});

// GET /api/isu/:jia_isu_uuid
// ISUの情報を取得
app.get(
  "/api/isu/:jia_isu_uuid",
  async (req: express.Request<{ jia_isu_uuid: string }>, res) => {
    const db = await pool.getConnection();
    try {
      const [jiaUserId, errStatusCode, err] = await getUserIdFromSession(
        req,
        db
      );
      if (err) {
        if (errStatusCode === 401) {
          return res.status(401).type("text").send("you are not signed in");
        }
        console.error(err);
        return res.status(500).send();
      }

      const jiaIsuUUID = req.params.jia_isu_uuid;
      const [[isu]] = await db.query<Isu[]>(
        "SELECT * FROM `isu` WHERE `jia_user_id` = ? AND `jia_isu_uuid` = ?",
        [jiaUserId, jiaIsuUUID]
      );
      if (!isu) {
        return res.status(404).type("text").send("not found: isu");
      }
      const isuResponse: IsuResponse = {
        id: isu.id,
        jia_isu_uuid: isu.jia_isu_uuid,
        name: isu.name,
        character: isu.character,
      };
      return res.status(200).json(isuResponse);
    } catch (err) {
      console.error(`db error: ${err}`);
      return res.status(500).send();
    } finally {
      db.release();
    }
  }
);

// ISUのコンディションの文字列からコンディションレベルを計算
function calculateConditionLevel(condition: string): [string, Error?] {
  var conditionLevel: string;
  const warnCount = (() => {
    let count = 0;
    let pos = 0;
    while (pos !== -1) {
      pos = condition.indexOf("=true", pos);
      if (pos >= 0) {
        count += 1;
        pos += 5;
      }
    }
    return count;
  })();
  switch (warnCount) {
    case 0:
      conditionLevel = conditionLevelInfo;
      break;
    case 1:
    case 2:
      conditionLevel = conditionLevelWarning;
      break;
    case 3:
      conditionLevel = conditionLevelCritical;
      break;
    default:
      return ["", new Error("unexpected warn count")];
  }
  return [conditionLevel, undefined];
}

app.listen(Number.parseInt(process.env["SERVER_APP_PORT"] ?? "3000"), () => {});
