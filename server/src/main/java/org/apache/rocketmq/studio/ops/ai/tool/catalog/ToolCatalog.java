/*
 * Licensed to the Apache Software Foundation (ASF) under one or more
 * contributor license agreements.  See the NOTICE file distributed with
 * this work for additional information regarding copyright ownership.
 * The ASF licenses this file to You under the Apache License, Version 2.0
 * (the "License"); you may not use this file except in compliance with
 * the License.  You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */
package org.apache.rocketmq.studio.ops.ai.tool.catalog;

import com.fasterxml.jackson.annotation.JsonIgnoreProperties;
import com.fasterxml.jackson.core.type.TypeReference;
import com.fasterxml.jackson.databind.ObjectMapper;
import com.fasterxml.jackson.dataformat.yaml.YAMLFactory;
import com.networknt.schema.Error;
import com.networknt.schema.InputFormat;
import com.networknt.schema.Schema;
import com.networknt.schema.SchemaRegistry;
import com.networknt.schema.SpecificationVersion;
import lombok.Getter;
import org.apache.rocketmq.studio.ops.ai.tool.core.ToolDefinition;
import org.apache.rocketmq.studio.ops.ai.tool.core.ToolError;
import org.springframework.beans.factory.annotation.Autowired;
import org.springframework.core.io.Resource;
import org.springframework.core.io.ResourceLoader;
import org.springframework.core.io.support.ResourcePatternResolver;
import org.springframework.core.io.support.ResourcePatternUtils;
import org.springframework.stereotype.Component;

import java.io.IOException;
import java.nio.charset.StandardCharsets;
import java.util.ArrayList;
import java.util.Arrays;
import java.util.Comparator;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Map;
import java.util.Optional;

@Component
public class ToolCatalog {

    static final String MANIFEST_RESOURCE = "classpath:tool-catalog/manifest.yaml";
    static final String SHARD_PATTERN = "classpath*:tool-catalog/tools/*.yaml";
    static final String SCHEMA_RESOURCE = "classpath:tool-catalog/rmq-tools.schema.json";

    private static final ObjectMapper YAML_MAPPER = new ObjectMapper(new YAMLFactory());
    private static final TypeReference<Map<String, Object>> MAP_TYPE = new TypeReference<Map<String, Object>>() {
    };

    @Getter
    private final String version;
    private final List<ToolDefinition> definitions;
    private final Map<String, ToolDefinition> definitionsByName;

    @Autowired
    public ToolCatalog(ResourceLoader resourceLoader) {
        ToolCatalog loaded = load(resourceLoader);
        this.version = loaded.version;
        this.definitions = loaded.definitions;
        this.definitionsByName = loaded.definitionsByName;
    }

    private ToolCatalog(
            String version,
            List<ToolDefinition> definitions,
            Map<String, ToolDefinition> definitionsByName) {
        this.version = version;
        this.definitions = definitions;
        this.definitionsByName = definitionsByName;
    }

    static ToolCatalog load(ResourceLoader resourceLoader) {
        try {
            Resource manifestResource = resourceLoader.getResource(MANIFEST_RESOURCE);
            Resource schemaResource = resourceLoader.getResource(SCHEMA_RESOURCE);
            byte[] manifestBytes = manifestResource.getContentAsByteArray();
            ManifestDocument manifest = YAML_MAPPER.readValue(manifestBytes, ManifestDocument.class);

            ResourcePatternResolver resourcePatternResolver =
                    ResourcePatternUtils.getResourcePatternResolver(resourceLoader);
            Resource[] shards = resourcePatternResolver.getResources(SHARD_PATTERN);
            List<Resource> shardResources = Arrays.stream(shards)
                    .sorted(Comparator.comparing(Resource::getFilename, Comparator.nullsFirst(Comparator.naturalOrder())))
                    .toList();
            if (shardResources.isEmpty()) {
                throw new IllegalStateException("Tool catalog contains no shards");
            }

            String schemaJson = schemaResource.getContentAsString(StandardCharsets.UTF_8);
            SchemaRegistry registry = SchemaRegistry.withDefaultDialect(
                    SpecificationVersion.DRAFT_2020_12);
            Schema schema = registry.getSchema(schemaJson, InputFormat.JSON);

            List<ToolDefinition> allTools = new ArrayList<>();
            for (Resource shard : shardResources) {
                byte[] shardBytes = shard.getContentAsByteArray();
                String shardYaml = new String(shardBytes, StandardCharsets.UTF_8);
                List<Error> shardErrors = new ArrayList<>(
                        schema.validate(shardYaml, InputFormat.YAML));
                if (!shardErrors.isEmpty()) {
                    shardErrors.sort(Comparator.comparing(
                            error -> error.getInstanceLocation().toString()));
                    throw new IllegalStateException(
                            "Tool catalog shard validation failed for " + shard.getFilename()
                                    + ": " + shardErrors);
                }
                Map<String, Object> shardMap = YAML_MAPPER.readValue(shardBytes, MAP_TYPE);
                Map<String, Object> expanded = expandShard(shardMap, manifest.defs());
                byte[] expandedBytes = YAML_MAPPER.writeValueAsBytes(expanded);
                ShardDocument shardDoc = YAML_MAPPER.readValue(expandedBytes, ShardDocument.class);
                if (!manifest.version().equals(shardDoc.version())) {
                    throw new IllegalStateException(
                            "Tool catalog shard " + shard.getFilename()
                                    + " version mismatch: expected " + manifest.version()
                                    + ", got " + shardDoc.version());
                }
                allTools.addAll(shardDoc.tools());
            }

            CatalogDocument document = new CatalogDocument(manifest.version(), allTools);
            return validatedCatalog(document);
        } catch (IOException e) {
            throw new IllegalStateException("Unable to load RocketMQ tool catalog", e);
        }
    }

    private static ToolCatalog validatedCatalog(CatalogDocument document) {
        Map<String, ToolDefinition> byName = new LinkedHashMap<>();
        for (ToolDefinition definition : document.tools()) {
            if (byName.putIfAbsent(definition.name(), definition) != null) {
                throw new IllegalStateException(
                        "Tool catalog contains duplicate tool name: " + definition.name());
            }
        }

        List<ToolDefinition> immutableDefinitions = List.copyOf(byName.values());
        return new ToolCatalog(
                document.version(),
                immutableDefinitions,
                Map.copyOf(byName));
    }

    // --- Schema expansion (inlined from former SchemaExpander) ---

    @SuppressWarnings("unchecked")
    private static Map<String, Object> expandShard(
            Map<String, Object> shard, Map<String, Object> defs) {
        if (defs == null) {
            defs = Map.of();
        }
        if (!(shard.get("tools") instanceof List<?> tools)) {
            return shard;
        }
        List<Object> expandedTools = new ArrayList<>(tools.size());
        for (Object tool : tools) {
            if (!(tool instanceof Map<?, ?> m)) {
                expandedTools.add(tool);
                continue;
            }
            Map<String, Object> t = new LinkedHashMap<>((Map<String, Object>) m);
            if (t.get("inputSchema") instanceof Map) {
                t.put("inputSchema", expandSchema(t.get("inputSchema"), defs));
            }
            if (t.get("outputSchema") instanceof Map) {
                t.put("outputSchema", expandSchema(t.get("outputSchema"), defs));
            }
            expandedTools.add(t);
        }
        Map<String, Object> result = new LinkedHashMap<>(shard);
        result.put("tools", expandedTools);
        return result;
    }

    @SuppressWarnings("unchecked")
    private static Map<String, Object> expandSchema(Object node, Map<String, Object> defs) {
        if (!(node instanceof Map<?, ?> raw)) {
            return Map.of();
        }
        Map<String, Object> schema = new LinkedHashMap<>((Map<String, Object>) raw);

        // $ref: resolve referenced def, then overlay own keys
        if (schema.remove("$ref") instanceof String ref) {
            Map<String, Object> resolved = expandSchema(resolveDef(ref, defs), defs);
            Map<String, Object> merged = new LinkedHashMap<>(resolved);
            merged.putAll(schema);
            schema = merged;
        }

        // allOf: merge each subschema into current
        if (schema.remove("allOf") instanceof List<?> allOf) {
            for (Object item : allOf) {
                mergeSchema(schema, expandSchema(item, defs));
            }
        }

        // Recurse into nested properties and items
        if (schema.get("properties") instanceof Map<?, ?> props) {
            Map<String, Object> expanded = new LinkedHashMap<>();
            props.forEach((k, v) -> expanded.put((String) k,
                    v instanceof Map ? expandSchema(v, defs) : v));
            schema.put("properties", expanded);
        }
        if (schema.get("items") instanceof Map<?, ?> items) {
            schema.put("items", expandSchema(items, defs));
        }
        return schema;
    }

    @SuppressWarnings("unchecked")
    private static void mergeSchema(Map<String, Object> target, Map<String, Object> source) {
        // properties: merge maps (target wins for duplicate keys)
        if (source.get("properties") instanceof Map<?, ?> srcProps) {
            Map<String, Object> merged = new LinkedHashMap<>(
                    (Map<String, Object>) target.getOrDefault("properties", Map.of()));
            merged.putAll((Map<String, Object>) srcProps);
            target.put("properties", merged);
        }
        // required: merge arrays with dedup
        if (source.get("required") instanceof List<?> srcReq) {
            List<String> merged = new ArrayList<>(
                    (List<String>) target.getOrDefault("required", List.of()));
            for (Object r : srcReq) {
                if (!merged.contains(r)) {
                    merged.add((String) r);
                }
            }
            target.put("required", merged);
        }
        // Everything else: source overwrites target
        source.forEach((k, v) -> {
            if (!k.equals("properties") && !k.equals("required")) {
                target.put(k, v);
            }
        });
    }

    @SuppressWarnings("unchecked")
    private static Map<String, Object> resolveDef(String ref, Map<String, Object> defs) {
        if (!ref.startsWith("#/$defs/")) {
            throw new IllegalStateException("Unsupported $ref: " + ref);
        }
        Object def = defs.get(ref.substring("#/$defs/".length()));
        if (def == null) {
            throw new IllegalStateException("Unknown $ref target: " + ref);
        }
        return (Map<String, Object>) def;
    }

    public List<ToolDefinition> list() {
        return definitions;
    }

    public Optional<ToolDefinition> find(String name) {
        return Optional.ofNullable(definitionsByName.get(name));
    }

    public ToolDefinition getDefinition(String name) {
        if (name == null || name.isBlank()) {
            throw ToolError.TOOL_NAME_REQUIRED.exception();
        }
        ToolDefinition definition = definitionsByName.get(name);
        if (definition == null) {
            throw ToolError.TOOL_NOT_FOUND.exception(name);
        }
        return definition;
    }

    @JsonIgnoreProperties(ignoreUnknown = true)
    private record ManifestDocument(String version, @com.fasterxml.jackson.annotation.JsonProperty("$defs") Map<String, Object> defs) {
    }

    private record ShardDocument(
            String version,
            List<ToolDefinition> tools) {
    }

    private record CatalogDocument(
            String version,
            List<ToolDefinition> tools) {
    }
}
