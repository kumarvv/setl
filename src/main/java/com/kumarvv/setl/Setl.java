/**
 * Copyright (c) 2016 Vijay Vijayaram
 *
 * Permission is hereby granted, free of charge, to any person obtaining a copy of this software
 * and associated documentation files (the "Software"), to deal in the Software without restriction,
 * including without limitation the rights to use, copy, modify, merge, publish, distribute, sublicense,
 * and/or sell copies of the Software, and to permit persons to whom the Software is furnished to do so,
 * subject to the following conditions:
 *
 * The above copyright notice and this permission notice shall be included in all copies or substantial
 * portions of the Software.
 *
 * THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR IMPLIED, INCLUDING BUT
 * NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY, FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT.
 * IN NO EVENT SHALL THE AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER LIABILITY,
 * WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM, OUT OF OR IN CONNECTION WITH
 * THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE SOFTWARE.
 */
package com.kumarvv.setl;

import com.kumarvv.setl.core.SetlProcessor;
import com.kumarvv.setl.model.Def;
import com.kumarvv.setl.model.Load;
import com.kumarvv.setl.model.Status;
import com.kumarvv.setl.utils.Chrono;
import org.apache.commons.lang3.StringUtils;
import org.codehaus.jackson.map.ObjectMapper;
import org.pmw.tinylog.Logger;
import org.pmw.tinylog.LoggingContext;

import java.nio.file.Path;
import java.nio.file.Paths;

public class Setl {
    private static final int STATUS_EVERY = 1000;

    /**
     * load definition file
     * @param filePath
     * @return
     */
    protected Def loadFile(String filePath) {
        ObjectMapper mapper = new ObjectMapper();
        try {
            Path path = Paths.get(filePath);
            final Def def = mapper.readValue(path.toFile(), Def.class);
            initCsvPaths(def, path);
            return def;
        } catch (Exception e) {
            Logger.error("Invalid definition file: {}", e.getMessage());
            Logger.trace(e);
        }
        return null;
    }

    /**
     * init csv paths
     *
     * @param def
     * @param defPath
     */
    protected void initCsvPaths(final Def def, Path defPath) {
        if (def.getExtract().getCsv() != null && StringUtils.isNotEmpty(def.getExtract().getCsv().getFile())) {
            Path csvPath = defPath.getParent().resolve(def.getExtract().getCsv().getFile()).normalize();
            def.getExtract().getCsv().setFilePath(csvPath.toAbsolutePath().toString());
        }

        for (Load load : def.getLoads()) {
            if (load.getPre() != null && load.getPre().getCsv() != null && StringUtils.isNotEmpty(load.getPre().getCsv().getFile())) {
                Path csvPath = defPath.getParent().resolve(load.getPre().getCsv().getFile()).normalize();
                load.getPre().getCsv().setFilePath(csvPath.toAbsolutePath().toString());
            }
            if (load.getPost() != null && load.getPost().getCsv() != null && StringUtils.isNotEmpty(load.getPost().getCsv().getFile())) {
                Path csvPath = defPath.getParent().resolve(load.getPost().getCsv().getFile()).normalize();
                load.getPost().getCsv().setFilePath(csvPath.toAbsolutePath().toString());
            }
        }
    }

    /**
     * start the process
     * @param def
     */
    protected void start(final Def def) {
        if (def == null) {
            Logger.error("Invalid or blank definition");
            return;
        }

        LoggingContext.put("defName", def.getName());
        LoggingContext.put("loadTable", "");

        Logger.info("Processing " + def.getName());

        Status status = new Status((s) -> printStatEvery(s));
        SetlProcessor processor = new SetlProcessor(status, def);
        Thread t = new Thread(processor);
        t.run();

        try {
            t.join();
        } catch (InterruptedException ie) {}

        printStat(status);
    }

    protected void printStatEvery(Status status) {
        if (status.getRowsFound() % STATUS_EVERY == 0) {
            Logger.info("Found: {}, Processed: {}", status.getRowsFound(), status.getRowsProcessed());
        }
    }

    protected void printStat(Status status) {
        Logger.info("Found: {}, Processed: {}", status.getRowsFound(), status.getRowsProcessed());
    }

    public static void main(String[] args) {
        Chrono kch = Chrono.start("all");
        String defPath = "src/main/resources/test.json";
//        String defPath = "src/main/resources/csv.json";
//        String defPath = "/Users/Vijay/Dev/projects/ktacs/java/ktacs/setl/master/country.json";

        Setl me = new Setl();
        Def def = me.loadFile(defPath);
        me.start(def);
        kch.stop();
        Logger.info("ALL DONE!");
    }

}