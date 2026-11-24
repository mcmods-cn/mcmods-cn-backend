import sys
from pathlib import Path
src=Path(sys.argv[1]).read_text()
# Preserve the native recipe catalogue and keyboard/rect/touch assertions. The
# synthetic three-kind fixture has no manual/mixed catalogue, only its geometry.
if sys.argv[3]=='geometry':
    src=src.replace("runtime.owner, 'mcmods-live-recipe-catalog'", "runtime.owner, 'mcmods-live-recipe-geometry'")
    src=src.replace("assert.equal(runtime.cont002.expectedAmount, 3);","").replace("assert.equal(runtime.cont002.expectedProbability, .5);","")
    first=src.index('    const context = await browser.newContext({')
    end=src.index('    const geometry = runtime.gr010?.cases')
    src=src[:first]+src[end:]
    src=src.replace("caseIds: ['CONT-002', 'CONT-003', 'GR-010']", "caseIds: ['GR-010-synthetic-v7-with-assets']").replace('PASS_RECORDED_REAL_RECIPE_CATALOG_DETAIL_QUANTITY_NAVIGATION_AND_AVAILABLE_GEOMETRY','PASS_NATIVE_SYNTHETIC_ITEM_FLUID_GAS_GEOMETRY')
src=src.replace("""            const rendered = p.waitForResponse(response => new URL(response.url()).pathname === '/api/v1/catalog/recipes/' + id + '/render');
            await p.goto(ownOrigin + '/recipe-types?recipePublicId=' + id);
            const response = await rendered;""", """            const [response] = await Promise.all([
                p.waitForResponse(response => new URL(response.url()).pathname === '/api/v1/catalog/recipes/' + id + '/render'),
                p.goto(ownOrigin + '/recipe-types?recipePublicId=' + id)
            ]);""")
Path(sys.argv[2]).write_text(src)
